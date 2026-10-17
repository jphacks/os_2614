"use client";

// すれ違いパイプラインの起点。signedIn の画面 (AppShell) から 1 つだけマウントし、
// - 権限の確認・要求 (bluetooth / location / notifications)
// - beacon token の取得と native への反映 (todo 23)
// - sighting の drain と送信 (todo 24)
// - 前面のみの位置取得と PUT /me/location (todo 25)
// をまとめて回す。signedOut / アンマウントで native を止める。
//
// 依存はすべて既存モジュール (beacon.ts / sightings.ts / hooks.ts) に寄せ、ここは
// ライフサイクル (開始・停止・前面復帰) だけを持つ。

import { useCallback, useEffect, useRef, useState } from "react";
import { syncBeaconTokensWithPlugin } from "./beacon";
import type { DevicePermission, PermissionStatus, ProximityStatus } from "./definitions";
import {
  useLocationUpload,
  useNearbyTokens,
  useProximityStatus,
  type LocationUploadState,
  type NearbyToken,
} from "./hooks";
import { ChainProximity } from "./index";
import {
  PROXIMITY_ALL_PERMISSIONS,
  PERMISSION_RECHECK_MS,
  PROXIMITY_SYNC_POLL_MS,
  TOKEN_REFRESH_RETRY_MS,
  describeProximityError,
  isForegroundResume,
  missingPermissions,
  proximityPermissionsReady,
  shouldRefreshTokens,
} from "./pipeline";
import { syncSightingsWithPlugin } from "./sightings";
import { SightingOutbox } from "./sync";
import { usePartyPresenceReporter } from "./party";

export interface ProximityPipeline {
  /** native の走行状態 (getStatus + statusChange)。 */
  status: ProximityStatus | null;
  /** 直近 windowMs に見えた token (近く画面用)。 */
  nearby: NearbyToken[];
  /** 位置の現在値と、PUT /me/location の結果。 */
  location: LocationUploadState;
  /** 権限の現在値。確認前は null。 */
  permissions: PermissionStatus | null;
  /** 走行に必要な権限が揃っているか。 */
  ready: boolean;
  /** まだ許可されていない必須権限。 */
  missing: DevicePermission[];
  /** 表示用のエラー (native の lastError / 位置送信 / 同期)。 */
  error: string | null;
  /** 権限を手動で要求する。自動リトライはしない。 */
  requestPermissions: () => void;
}

function describeError(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}

export function useProximityPipeline(): ProximityPipeline {
  const status = useProximityStatus();
  const onPartySighting = usePartyPresenceReporter({
    enabled: status?.running === true,
    clockOffsetMs: status?.clockOffsetMs ?? 0,
  });
  const nearby = useNearbyTokens(45_000, onPartySighting);
  const [permissions, setPermissions] = useState<PermissionStatus | null>(null);
  const [syncError, setSyncError] = useState<string | null>(null);
  const outboxRef = useRef<SightingOutbox | null>(null);
  const statusRef = useRef<ProximityStatus | null>(status);
  const hiddenRef = useRef(false);

  useEffect(() => {
    statusRef.current = status;
  });

  useEffect(() => {
    let active = true;
    ChainProximity.checkPermissions()
      .then((next) => {
        if (active) {
          setPermissions(next);
        }
      })
      .catch(() => {
        // 権限を読めない環境でも、要求 UI は出せるように未許可として扱う。
        if (active) {
          setPermissions({ bluetooth: "prompt", location: "prompt", notifications: "prompt" });
        }
      });
    return () => {
      active = false;
    };
  }, []);

  const ready = proximityPermissionsReady(permissions);

  // 権限は他の画面 (ちかくの「位置を有効にする」) や OS の設定でも変わる。
  // 揃うまでは読み直して、許可された時点で走行と位置送信を始める。
  useEffect(() => {
    if (ready) {
      return;
    }
    let active = true;
    const recheck = () => {
      ChainProximity.checkPermissions()
        .then((next) => {
          if (active) {
            setPermissions(next);
          }
        })
        .catch(() => undefined);
    };
    const timer = setInterval(recheck, PERMISSION_RECHECK_MS);
    document.addEventListener("visibilitychange", recheck);
    return () => {
      active = false;
      clearInterval(timer);
      document.removeEventListener("visibilitychange", recheck);
    };
  }, [ready]);
  // 位置はグループ表示 (403 location_stale) に要るので、Bluetooth の許可を待たず送る。
  const location = useLocationUpload({ enabled: permissions?.location === "granted" });

  useEffect(() => {
    if (!ready) {
      return;
    }

    const outbox = (outboxRef.current ??= new SightingOutbox());
    const controller = new AbortController();
    const lastFlushAt = { value: 0 };
    const rateLimitedUntil = { value: 0 };
    let cancelled = false;
    hiddenRef.current = document.hidden;

    const tokenSync = { lastAttemptAt: 0 };
    const syncTokens = () => {
      tokenSync.lastAttemptAt = Date.now();
      return syncBeaconTokensWithPlugin({ signal: controller.signal }).then(
        () => {
          if (!cancelled) {
            setSyncError(null);
          }
        },
        (cause: unknown) => {
          if (!cancelled) {
            setSyncError(describeError(cause));
          }
        },
      );
    };

    // 走りっぱなしでも token が尽きないよう、残りが減ったら取り直す。
    const refreshTokensIfNeeded = () => {
      const current = statusRef.current;
      if (current === null) {
        return;
      }
      const nowMs = Date.now();
      if (nowMs - tokenSync.lastAttemptAt < TOKEN_REFRESH_RETRY_MS) {
        return;
      }
      if (
        shouldRefreshTokens({
          tokensValidUntil: current.tokensValidUntil,
          lastError: current.lastError,
          nowMs,
        })
      ) {
        void syncTokens();
      }
    };

    const syncSightings = async (foreground: boolean) => {
      try {
        const result = await syncSightingsWithPlugin({
          outbox,
          running: statusRef.current?.running ?? false,
          foreground,
          lastFlushAtMs: lastFlushAt.value,
          rateLimitedUntilMs: rateLimitedUntil.value,
          signal: controller.signal,
        });
        if (cancelled) {
          return;
        }
        if (result.flush === null) {
          return;
        }
        lastFlushAt.value = Date.now();
        rateLimitedUntil.value =
          result.flush.retryAfterMs === null ? 0 : Date.now() + result.flush.retryAfterMs;
        if (result.flush.stopped === "error") {
          setSyncError(describeError(result.flush.error));
        } else if (result.flush.stopped !== "rate_limited") {
          setSyncError(null);
        }
      } catch (cause) {
        if (!cancelled) {
          setSyncError(describeError(cause));
        }
      }
    };

    // token を取ってから native を起動する。token が無いと発信できない。
    void (async () => {
      await syncTokens();
      if (cancelled) {
        return;
      }
      try {
        await ChainProximity.start();
      } catch (cause) {
        // 権限不足など。自動リトライはせず、UI からの手動再要求に任せる。
        if (!cancelled) {
          setSyncError(describeError(cause));
        }
        return;
      }
      if (!cancelled) {
        void syncSightings(false);
      }
    })();

    const interval = setInterval(() => {
      refreshTokensIfNeeded();
      void syncSightings(false);
    }, PROXIMITY_SYNC_POLL_MS);

    const onVisibilityChange = () => {
      const hidden = document.hidden;
      const resumed = isForegroundResume(hiddenRef.current, hidden);
      hiddenRef.current = hidden;
      if (!resumed) {
        return;
      }
      void syncTokens();
      void syncSightings(true);
    };
    document.addEventListener("visibilitychange", onVisibilityChange);

    return () => {
      cancelled = true;
      clearInterval(interval);
      document.removeEventListener("visibilitychange", onVisibilityChange);
      controller.abort();
      void ChainProximity.stop().catch(() => undefined);
    };
  }, [ready]);

  const requestPermissions = useCallback(() => {
    ChainProximity.requestPermissions({ permissions: [...PROXIMITY_ALL_PERMISSIONS] })
      .then((next) => {
        setPermissions(next);
        setSyncError(null);
      })
      .catch((cause: unknown) => {
        setSyncError(describeError(cause));
      });
  }, []);

  // 再取得の失敗理由が lastError (tokens_exhausted など) に隠れないよう syncError を先に見る。
  const error =
    syncError ??
    describeProximityError(status?.lastError ?? null) ??
    location.uploadError ??
    location.error;

  return {
    status,
    nearby,
    location,
    permissions,
    ready,
    missing: missingPermissions(permissions),
    error,
    requestPermissions,
  };
}
