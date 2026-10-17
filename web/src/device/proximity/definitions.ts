// ChainProximity プラグインの契約。native (android/.../proximity/) と web モック (web.ts) の両方がこれを実装する。
// specification/android/README.md の ChainProximity のうち「端末機能」だけを持つ版。
// backend 通信 (token 取得, sighting 送信, 位置送信) とセッションは JS 側が担い、native は持たない。

import type { PermissionState, PluginListenerHandle } from "@capacitor/core";

export type DevicePermission = "bluetooth" | "location" | "notifications";

export interface PermissionStatus {
  /** BLUETOOTH_SCAN + BLUETOOTH_ADVERTISE (Android 12+)。11 以下は位置権限と同じ値 */
  bluetooth: PermissionState;
  /** ACCESS_FINE_LOCATION */
  location: PermissionState;
  /** POST_NOTIFICATIONS (Android 13+)。12 以下は常に granted */
  notifications: PermissionState;
}

/** backend `GET /beacon/tokens` の 1 要素 (specification/backend/05 §2.2) */
export interface BeaconToken {
  /** ISO 8601。窓の開始時刻 (backend の時計) */
  windowStart: string;
  /** base64url, 16 バイト */
  token: string;
}

export type ProximityError =
  | "permission_revoked"
  | "bluetooth_off"
  | "bluetooth_unsupported"
  | "advertise_failed"
  | "scan_failed"
  | "tokens_exhausted";

export interface ProximityStatus {
  /** すれ違いモード (Foreground Service) が動いている */
  running: boolean;
  bluetoothEnabled: boolean;
  advertising: boolean;
  scanning: boolean;
  /** いま発信中の token の窓。発信していなければ null */
  currentWindowStart: string | null;
  /** 手持ちの token が尽きる時刻。null は token なし */
  tokensValidUntil: string | null;
  /** drainSightings でまだ取り出していない token の数 */
  pendingSightings: number;
  clockOffsetMs: number;
  lastError: ProximityError | null;
}

/** token ごとの集約。backend `POST /encounters/sightings` の 1 要素とそのまま対応する */
export interface Sighting {
  token: string;
  firstSeenAt: string;
  lastSeenAt: string;
  maxRssi: number;
  count: number;
}

/** 受信のたびの通知 (同じ token は 5 秒に 1 回まで)。WebView が止まっている間は届かない */
export interface SightingEvent {
  token: string;
  rssi: number;
  seenAt: string;
}

export interface LocationFix {
  lat: number;
  lon: number;
  accuracyM: number;
  /** ISO 8601 (clockOffsetMs 補正済み) */
  recordedAt: string;
  isMock: boolean;
}

export interface ChainProximityPlugin {
  // ---- 権限 ----
  checkPermissions(): Promise<PermissionStatus>;
  /** 省略時は 3 つとも要求する */
  requestPermissions(opts?: { permissions?: DevicePermission[] }): Promise<PermissionStatus>;

  // ---- すれ違い (BLE) ----
  /** 発信する token を渡す。既存の手持ちは置き換える。走行中なら即座に反映する */
  setBeaconTokens(opts: { windowSec: number; tokens: BeaconToken[] }): Promise<void>;
  /** backend の時計 - 端末の時計 (ミリ秒)。窓の切替と、返すすべての時刻に足す */
  setClockOffset(opts: { offsetMs: number }): Promise<void>;
  /** Foreground Service を起動し advertise と scan を始める。前面からのみ呼べる。権限が無ければ reject */
  start(): Promise<void>;
  stop(): Promise<void>;
  getStatus(): Promise<ProximityStatus>;
  /** たまった sighting を返して native 側から消す。送信に失敗したら JS 側で保持すること */
  drainSightings(): Promise<{ items: Sighting[] }>;

  // ---- 位置 (前面のみ) ----
  /** 1 回だけ取得する。権限が無い / 取得できない場合は reject (code: "denied" | "unavailable" | "timeout") */
  getCurrentLocation(opts?: { timeoutMs?: number; maxAgeMs?: number }): Promise<LocationFix>;
  /** intervalMs ごとに "location" イベントを出す。アプリが背面に回ると止まる */
  startLocationUpdates(opts: { intervalMs: number }): Promise<void>;
  stopLocationUpdates(): Promise<void>;

  addListener(
    eventName: "statusChange",
    listener: (status: ProximityStatus) => void,
  ): Promise<PluginListenerHandle>;
  addListener(
    eventName: "sighting",
    listener: (event: SightingEvent) => void,
  ): Promise<PluginListenerHandle>;
  addListener(
    eventName: "location",
    listener: (fix: LocationFix) => void,
  ): Promise<PluginListenerHandle>;
  removeAllListeners(): Promise<void>;
}

/** BLE の manufacturer data (specification/backend/05 §2.3)。web モックと native で共有する値 */
export const BEACON_COMPANY_ID = 0xffff;
export const BEACON_MAGIC = [0x43, 0x48] as const;
export const BEACON_VERSION = 0x01;
