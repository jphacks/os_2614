"use client";

import { useEffect, useRef, useState } from "react";
import type { PluginListenerHandle } from "@capacitor/core";
import { ApiError } from "@/auth/errors";
import { ChainProximity } from "./index";
import type { LocationFix, ProximityStatus, SightingEvent } from "./definitions";
import {
  LOCATION_FOREGROUND_INTERVAL_MS,
  locationSkipReason,
  describeSkipReason,
  locationUploadDue,
  onLocationUploaded,
  uploadLocation,
  type LocationSender,
} from "./location";

const NEARBY_PRUNE_INTERVAL_MS = 5_000;

/** useNearbyTokens が返す 1 件。rssi は直近の観測値。 */
export interface NearbyToken {
  token: string;
  rssi: number;
  /** ISO 8601 (clockOffsetMs 補正済み)。SightingEvent.seenAt をそのまま持つ */
  lastSeenAt: string;
}

type NearbyRecord = NearbyToken & {
  /** 端末時計での観測時刻。windowMs の判定にだけ使う */
  seenAtLocalMs: number;
};

/**
 * すれ違いモードの現在の状態。getStatus の初期値に statusChange を重ねる。
 * native も web も同じ ProximityStatus を返す。
 */
export function useProximityStatus(): ProximityStatus | null {
  const [status, setStatus] = useState<ProximityStatus | null>(null);

  useEffect(() => {
    let active = true;
    let handle: PluginListenerHandle | undefined;
    let disposed = false;

    ChainProximity.getStatus()
      .then((next) => {
        if (active) {
          setStatus(next);
        }
      })
      .catch(() => {
        // getStatus が使えない環境では null のままにする。
      });

    ChainProximity.addListener("statusChange", (next) => {
      setStatus(next);
    })
      .then((registered) => {
        // addListener の解決より先にアンマウントされたら、その場で外す
        if (disposed) {
          void registered.remove();
        } else {
          handle = registered;
        }
      })
      .catch(() => {
        // リスナーを張れない環境では初期値のままにする。
      });

    return () => {
      active = false;
      disposed = true;
      void handle?.remove();
    };
  }, []);

  return status;
}

/**
 * 直近 windowMs 以内に見えた token の一覧 (rssi 降順)。
 * sighting イベントを集約し、期限切れは定期的に落とす。
 */
export function useNearbyTokens(windowMs = 45_000): NearbyToken[] {
  const [tokens, setTokens] = useState<NearbyToken[]>([]);
  const records = useRef(new Map<string, NearbyRecord>());

  useEffect(() => {
    let handle: PluginListenerHandle | undefined;
    let disposed = false;
    const store = records.current;

    const publish = () => {
      const cutoff = Date.now() - windowMs;
      for (const [token, record] of store) {
        if (record.seenAtLocalMs < cutoff) {
          store.delete(token);
        }
      }
      const next = [...store.values()]
        .map((record) => ({
          token: record.token,
          rssi: record.rssi,
          lastSeenAt: record.lastSeenAt,
        }))
        .sort((a, b) => b.rssi - a.rssi);
      setTokens(next);
    };

    ChainProximity.addListener("sighting", (event: SightingEvent) => {
      store.set(event.token, {
        token: event.token,
        rssi: event.rssi,
        lastSeenAt: event.seenAt,
        seenAtLocalMs: Date.now(),
      });
      publish();
    })
      .then((registered) => {
        // addListener の解決より先にアンマウントされたら、その場で外す
        if (disposed) {
          void registered.remove();
        } else {
          handle = registered;
        }
      })
      .catch(() => {
        // リスナーを張れない環境では空のままにする。
      });

    const interval = setInterval(publish, NEARBY_PRUNE_INTERVAL_MS);

    return () => {
      clearInterval(interval);
      store.clear();
      disposed = true;
      void handle?.remove();
    };
  }, [windowMs]);

  return tokens;
}

/** getCurrentLocation / geolocation の失敗を表示用の文字列にする。 */
function describeError(error: unknown): string {
  if (error instanceof ApiError && error.status === 400) {
    // 「リクエストが不正です」だけでは原因が分からないので、backend の fields を添える。
    const fields = error.details?.fields;
    if (fields && typeof fields === "object") {
      return `${error.message} (${Object.entries(fields).map(([k, v]) => `${k}: ${String(v)}`).join(", ")})`;
    }
  }
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

/**
 * startLocationUpdates は plugin 側にタイマーが 1 本しかない (web.ts の locationTimer)。
 * 画面の現在地 (エリア作成など) と近接パイプラインが同時に useLocation しても互いの
 * interval を奪い合わないよう、購読者のうち最短の interval で 1 本だけ回す。
 */
interface LocationUpdatesDriver {
  subscribers: Map<symbol, number>;
  activeIntervalMs: number;
}

let locationUpdatesDriver: LocationUpdatesDriver | null = null;

function startLocationUpdatesAt(
  driver: LocationUpdatesDriver,
  intervalMs: number,
  onError: (cause: unknown) => void,
): void {
  driver.activeIntervalMs = intervalMs;
  ChainProximity.startLocationUpdates({ intervalMs }).catch(onError);
}

function joinLocationUpdates(
  intervalMs: number,
  onError: (cause: unknown) => void,
): () => void {
  const key = Symbol("location-updates");
  const driver =
    locationUpdatesDriver ??
    (locationUpdatesDriver = { subscribers: new Map<symbol, number>(), activeIntervalMs: 0 });
  driver.subscribers.set(key, intervalMs);

  const minIntervalMs = Math.min(...driver.subscribers.values());
  if (minIntervalMs !== driver.activeIntervalMs) {
    startLocationUpdatesAt(driver, minIntervalMs, onError);
  }

  return () => {
    const current = locationUpdatesDriver;
    if (current === null) {
      return;
    }
    current.subscribers.delete(key);
    if (current.subscribers.size === 0) {
      locationUpdatesDriver = null;
      current.activeIntervalMs = 0;
      void ChainProximity.stopLocationUpdates().catch(() => {
        // すでに止まっている場合は無視する。
      });
      return;
    }
    const nextMin = Math.min(...current.subscribers.values());
    if (nextMin !== current.activeIntervalMs) {
      startLocationUpdatesAt(current, nextMin, () => {
        // 残っている購読者は自分の error で状態を持つ。
      });
    }
  };
}

/**
 * 位置の現在値。enabled の間だけ startLocationUpdates を回し、
 * "location" イベントの最新値を返す。アンマウントで止める。
 */
export function useLocation(opts: { intervalMs: number; enabled: boolean }): {
  fix: LocationFix | null;
  error: string | null;
} {
  const { intervalMs, enabled } = opts;
  const [fix, setFix] = useState<LocationFix | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    let handle: PluginListenerHandle | undefined;
    let disposed = false;

    ChainProximity.addListener("location", (next) => {
      setFix(next);
      setError(null);
    })
      .then((registered) => {
        // addListener の解決より先にアンマウントされたら、その場で外す
        if (disposed) {
          void registered.remove();
        } else {
          handle = registered;
        }
      })
      .catch((cause) => {
        setError(describeError(cause));
      });

    const leave = joinLocationUpdates(intervalMs, (cause) => {
      setError(describeError(cause));
    });

    // 定期取得の最初の値は intervalMs 後になる。画面を開いた直後に 403 location_stale に
    // ならないよう、現在地を 1 回すぐ取る。
    // maxAge 0: 古い低精度のキャッシュを送ると、次の fix との間で移動速度超過 (400) になりうる。
    ChainProximity.getCurrentLocation({ maxAgeMs: 0 })
      .then((next) => {
        if (!disposed) {
          setFix((prev) => prev ?? next);
          setError(null);
        }
      })
      .catch((cause) => {
        if (!disposed) {
          setError(describeError(cause));
        }
      });

    return () => {
      disposed = true;
      void handle?.remove();
      leave();
    };
  }, [enabled, intervalMs]);

  return { fix, error };
}

/** useLocationUpload が返す状態。 */
export interface LocationUploadState {
  /** 位置の現在値 (アップロード可否に関わらず最新の取得値)。 */
  fix: LocationFix | null;
  /** 位置の取得が続かない / 権限が無いときの理由。 */
  error: string | null;
  /** 直近のアップロード失敗。成功で消える。 */
  uploadError: string | null;
  /** 直近にアップロードした時刻 (ms)。未送信は 0。 */
  lastUploadedAtMs: number;
}

export interface LocationUploadOptions {
  /** true の間だけ位置を取り、backend へ送る (前面のみ)。 */
  enabled: boolean;
  /** 位置の取得間隔。既定は前面用の 30 秒。 */
  intervalMs?: number;
  /** 送信間隔の下限。既定 10 秒 (location 6/分)。 */
  minIntervalMs?: number;
  /** 送信の差し替え。テスト用。既定はデータ層の uploadLocation。 */
  sender?: LocationSender;
}

/**
 * 位置を取り、backend の受理条件を満たす値だけを `PUT /me/location` へ送る (05 §4)。
 *
 * - 取得は useLocation に任せる (enabled=false で停止、アンマウントで stopLocationUpdates)。
 * - 送信は minIntervalMs (既定 10 秒) をあけて行い、location 6/分のレート制限を超えない。
 * - isMock / 精度超過 / 古すぎ・未来すぎの fix は送らない (backend が 400 にするため)。
 */
export function useLocationUpload(opts: LocationUploadOptions): LocationUploadState {
  const { enabled, intervalMs = LOCATION_FOREGROUND_INTERVAL_MS, minIntervalMs, sender } = opts;
  const { fix, error } = useLocation({ intervalMs, enabled });

  const lastSentAtRef = useRef(0);
  const [upload, setUpload] = useState<{
    uploadError: string | null;
    lastUploadedAtMs: number;
  }>({ uploadError: null, lastUploadedAtMs: 0 });

  useEffect(() => {
    if (!enabled || !fix) {
      return;
    }
    const nowMs = Date.now();
    if (locationSkipReason(fix, nowMs) !== null) {
      return;
    }
    if (!locationUploadDue({ lastSentAtMs: lastSentAtRef.current, nowMs, minIntervalMs })) {
      return;
    }
    lastSentAtRef.current = nowMs;

    let active = true;
    const controller = new AbortController();
    const send = sender ?? uploadLocation;
    void send(fix, { signal: controller.signal })
      .then(() => {
        if (active) {
          setUpload({ uploadError: null, lastUploadedAtMs: nowMs });
        }
      })
      .catch((cause) => {
        if (active) {
          setUpload({ uploadError: describeError(cause), lastUploadedAtMs: nowMs });
        }
      });

    return () => {
      active = false;
      controller.abort();
    };
  }, [enabled, fix, minIntervalMs, sender]);

  // 送れない fix を黙って捨てると、位置が届かない理由が分からない。表示用に導出する。
  const skipReason = fix ? locationSkipReason(fix, fix.recordedAt ? Date.parse(fix.recordedAt) + 1 : 0) : null;
  const skipError = skipReason === "mock" || skipReason === "low_accuracy" ? describeSkipReason(skipReason) : null;

  return {
    fix,
    error,
    uploadError: skipError ?? upload.uploadError,
    lastUploadedAtMs: upload.lastUploadedAtMs,
  };
}

/**
 * active の間、位置の送信が成功するたびに reload を呼ぶ。
 * 画面を開いた直後は位置がまだ backend に届いておらず 403 location_stale になるため、
 * 届いたら取り直して「位置情報が必要です」の表示を解く。
 */
export function useReloadOnLocationUpload(active: boolean, reload: () => void): void {
  const reloadRef = useRef(reload);
  useEffect(() => {
    reloadRef.current = reload;
  });

  useEffect(() => {
    if (!active) {
      return;
    }
    return onLocationUploaded(() => reloadRef.current());
  }, [active]);
}
