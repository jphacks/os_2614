"use client";

import { useEffect, useRef, useState } from "react";
import type { PluginListenerHandle } from "@capacitor/core";
import { ChainProximity } from "./index";
import type { LocationFix, ProximityStatus, SightingEvent } from "./definitions";

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
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
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

    ChainProximity.startLocationUpdates({ intervalMs }).catch((cause) => {
      setError(describeError(cause));
    });

    return () => {
      disposed = true;
      void handle?.remove();
      void ChainProximity.stopLocationUpdates().catch(() => {
        // すでに止まっている場合は無視する。
      });
    };
  }, [enabled, intervalMs]);

  return { fix, error };
}
