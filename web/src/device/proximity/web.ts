// ブラウザ (pnpm dev) 用の ChainProximity モック。
// BLE と GPS を持たないので、走行中は偽の sighting を定期的に発火し、
// 位置は navigator.geolocation をそのまま使う。native (Android) と同じ
// 状態遷移・イベント・エラーの形を返すことを目的とする。

import { WebPlugin } from "@capacitor/core";
import type { PermissionState } from "@capacitor/core";
import type {
  BeaconToken,
  ChainProximityPlugin,
  DevicePermission,
  LocationAccuracySettings,
  LocationFix,
  PermissionStatus,
  ProximityError,
  ProximityStatus,
  Sighting,
  SightingEvent,
} from "./definitions";

const DEFAULT_WINDOW_SEC = 900;
const DEFAULT_LOCATION_INTERVAL_MS = 30_000;

const SIGHTING_MIN_RSSI = -95;
const SIGHTING_MAX_RSSI = -45;
const SCAN_MIN_MS = 2_000;
const SCAN_MAX_MS = 6_000;

// base64url 16 バイト相当の固定値。中身はでたらめだが形式は backend 05 §2.1 と同じ。
const FAKE_TOKENS = [
  "DyvOJdd2_fBYcs9ZC8fKoA",
  "Pe78RXfRNy4MyWGPcFZdTg",
  "zQjMDbLD1utb1T0Mfg4Rcw",
  "z14n1-L6pzSqa2h1ej4_RQ",
  "qbq5Nf-RmtviS2lycM227g",
] as const;

type LocationErrorCode = "denied" | "unavailable" | "timeout";

/** getCurrentLocation が reject するときの Error に付く code。 */
export class LocationError extends Error {
  readonly code: LocationErrorCode;

  constructor(code: LocationErrorCode, message: string) {
    super(message);
    this.name = "LocationError";
    this.code = code;
  }
}

export class ChainProximityWeb extends WebPlugin implements ChainProximityPlugin {
  private running = false;
  private bluetoothEnabled = true;
  private advertising = false;
  private scanning = false;
  private currentWindowStart: string | null = null;
  private tokensValidUntil: string | null = null;
  private clockOffsetMs = 0;
  private lastError: ProximityError | null = null;

  private windowSec = DEFAULT_WINDOW_SEC;
  private tokens: BeaconToken[] = [];

  private readonly sightings = new Map<string, Sighting>();

  private scanTimer: ReturnType<typeof setTimeout> | null = null;
  private locationTimer: ReturnType<typeof setInterval> | null = null;

  // ---- 権限 ----

  async checkPermissions(): Promise<PermissionStatus> {
    return {
      bluetooth: "granted",
      notifications: "granted",
      location: await this.queryLocationPermission(),
    };
  }

  async requestPermissions(opts?: { permissions?: DevicePermission[] }): Promise<PermissionStatus> {
    void opts;
    return this.checkPermissions();
  }

  // ---- すれ違い (BLE) ----

  async setBeaconTokens(opts: { windowSec: number; tokens: BeaconToken[] }): Promise<void> {
    this.windowSec = opts.windowSec > 0 ? opts.windowSec : DEFAULT_WINDOW_SEC;
    this.tokens = [...opts.tokens].sort(
      (a, b) => Date.parse(a.windowStart) - Date.parse(b.windowStart),
    );
    this.refreshBeaconState();
    this.emitStatus();
  }

  async setClockOffset(opts: { offsetMs: number }): Promise<void> {
    this.clockOffsetMs = opts.offsetMs;
    if (this.running) {
      this.refreshBeaconState();
    }
    this.emitStatus();
  }

  async start(): Promise<void> {
    if (this.running) {
      return;
    }
    this.running = true;
    this.scanning = true;
    this.bluetoothEnabled = true;
    this.refreshBeaconState();
    this.startScanLoop();
    this.emitStatus();
  }

  async stop(): Promise<void> {
    this.running = false;
    this.scanning = false;
    this.advertising = false;
    this.currentWindowStart = null;
    this.stopScanLoop();
    this.stopLocationLoop();
    this.emitStatus();
  }

  async getStatus(): Promise<ProximityStatus> {
    return this.buildStatus();
  }

  async drainSightings(): Promise<{ items: Sighting[] }> {
    const items = [...this.sightings.values()].map((sighting) => ({ ...sighting }));
    this.sightings.clear();
    this.emitStatus();
    return { items };
  }

  // ---- 位置 (前面のみ) ----

  async getCurrentLocation(opts?: {
    timeoutMs?: number;
    maxAgeMs?: number;
    desiredAccuracyM?: number;
  }): Promise<LocationFix> {
    // desiredAccuracyM は使わない。ブラウザの geolocation は精度を待たずに 1 回で返す。
    const geolocation = this.getGeolocation();
    const timeoutMs = opts?.timeoutMs;
    return new Promise<LocationFix>((resolve, reject) => {
      geolocation.getCurrentPosition(
        (position) => resolve(this.toLocationFix(position)),
        (error) => reject(this.toLocationError(error)),
        timeoutMs !== undefined ? { timeout: timeoutMs } : undefined,
      );
    });
  }

  async startLocationUpdates(opts: { intervalMs: number }): Promise<void> {
    this.stopLocationLoop();
    const intervalMs = opts.intervalMs > 0 ? opts.intervalMs : DEFAULT_LOCATION_INTERVAL_MS;
    this.locationTimer = setInterval(() => {
      this.getCurrentLocation()
        .then((fix) => this.notifyListeners("location", fix))
        .catch(() => {
          // 1 回失敗しても次の interval で再試行する。エラーは getCurrentLocation 側で扱う。
        });
    }, intervalMs);
  }

  async stopLocationUpdates(): Promise<void> {
    this.stopLocationLoop();
  }

  async getLocationAccuracySettings(): Promise<LocationAccuracySettings> {
    // ブラウザからは端末設定を読めないので、案内を出さない値を返す。
    return { locationEnabled: true, wifiScanAvailable: true };
  }

  async openLocationSettings(): Promise<void> {
    // ブラウザには開く先が無い。
  }

  async removeAllListeners(): Promise<void> {
    this.stopScanLoop();
    this.stopLocationLoop();
    await super.removeAllListeners();
  }

  // ---- 内部 ----

  private now(): number {
    return Date.now() + this.clockOffsetMs;
  }

  private buildStatus(): ProximityStatus {
    return {
      running: this.running,
      bluetoothEnabled: this.bluetoothEnabled,
      advertising: this.advertising,
      scanning: this.scanning,
      currentWindowStart: this.currentWindowStart,
      tokensValidUntil: this.tokensValidUntil,
      pendingSightings: this.sightings.size,
      clockOffsetMs: this.clockOffsetMs,
      lastError: this.lastError,
    };
  }

  private emitStatus(): void {
    this.notifyListeners("statusChange", this.buildStatus());
  }

  private refreshBeaconState(): void {
    const now = this.now();
    let current: BeaconToken | null = null;
    let latestEnd = Number.NEGATIVE_INFINITY;

    for (const token of this.tokens) {
      const start = Date.parse(token.windowStart);
      if (!Number.isFinite(start)) {
        continue;
      }
      const end = start + this.windowSec * 1000;
      if (start <= now && now < end && (current === null || start > Date.parse(current.windowStart))) {
        current = token;
      }
      latestEnd = Math.max(latestEnd, end);
    }

    this.advertising = this.running && current !== null;
    this.currentWindowStart = this.advertising && current ? current.windowStart : null;
    this.tokensValidUntil = Number.isFinite(latestEnd) ? new Date(latestEnd).toISOString() : null;
    this.lastError = this.running && current === null ? "tokens_exhausted" : null;
  }

  private startScanLoop(): void {
    this.stopScanLoop();
    const schedule = () => {
      const delay = SCAN_MIN_MS + Math.random() * (SCAN_MAX_MS - SCAN_MIN_MS);
      this.scanTimer = setTimeout(() => {
        if (!this.running || !this.scanning) {
          return;
        }
        this.recordSighting();
        schedule();
      }, delay);
    };
    schedule();
  }

  private stopScanLoop(): void {
    if (this.scanTimer !== null) {
      clearTimeout(this.scanTimer);
      this.scanTimer = null;
    }
  }

  private stopLocationLoop(): void {
    if (this.locationTimer !== null) {
      clearInterval(this.locationTimer);
      this.locationTimer = null;
    }
  }

  private recordSighting(): void {
    const token = FAKE_TOKENS[Math.floor(Math.random() * FAKE_TOKENS.length)];
    const rssi =
      SIGHTING_MIN_RSSI +
      Math.floor(Math.random() * (SIGHTING_MAX_RSSI - SIGHTING_MIN_RSSI + 1));
    const seenAt = new Date(this.now()).toISOString();
    const event: SightingEvent = { token, rssi, seenAt };

    const existing = this.sightings.get(token);
    if (existing) {
      existing.lastSeenAt = seenAt;
      existing.maxRssi = Math.max(existing.maxRssi, rssi);
      existing.count += 1;
    } else {
      this.sightings.set(token, {
        token,
        firstSeenAt: seenAt,
        lastSeenAt: seenAt,
        maxRssi: rssi,
        count: 1,
      });
    }

    this.notifyListeners("sighting", event);
    this.emitStatus();
  }

  private async queryLocationPermission(): Promise<PermissionState> {
    if (typeof navigator === "undefined" || !navigator.permissions) {
      return "prompt";
    }
    try {
      const result = await navigator.permissions.query({ name: "geolocation" });
      return result.state;
    } catch {
      return "prompt";
    }
  }

  private getGeolocation(): Geolocation {
    if (typeof navigator === "undefined" || !navigator.geolocation) {
      throw new LocationError("unavailable", "geolocation is not available");
    }
    return navigator.geolocation;
  }

  private toLocationFix(position: GeolocationPosition): LocationFix {
    return {
      lat: position.coords.latitude,
      lon: position.coords.longitude,
      accuracyM: position.coords.accuracy,
      recordedAt: new Date(this.now()).toISOString(),
      isMock: false,
    };
  }

  private toLocationError(error: GeolocationPositionError): LocationError {
    if (error.code === error.PERMISSION_DENIED) {
      return new LocationError("denied", error.message || "location permission denied");
    }
    if (error.code === error.TIMEOUT) {
      return new LocationError("timeout", error.message || "location request timed out");
    }
    return new LocationError("unavailable", error.message || "location unavailable");
  }
}
