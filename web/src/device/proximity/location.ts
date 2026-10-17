// 位置のアップロード (backend 05 §4)。前面のみ。
//
// - native の startLocationUpdates はアプリが背面に回ると止まる (definitions.ts:96) ので、
//   フックが enabled の間だけ動かせば「前面のみ」という要件を満たす。
// - 送信はデータ層 (putJson) 経由。画面から直接 fetch しない。
// - backend は 400 を返す条件 (accuracyM > 200、recordedAt が [now-120s, now+30s] の外、
//   isMock=true) があるため、その条件を満たさない fix はそもそも送らない
//   (精度が悪い / 古い値を無条件に送らない)。
// - レート制限 location = 6 回/分 (02 §5)。1 リクエストあたり最短 10 秒あけて超えないようにする。

import { putJson } from "@/data/api";
import type { LocationFix } from "./definitions";

/** backend 05 §4: 受理される精度の上限。 */
export const LOCATION_MAX_ACCURACY_M = 200;
/** backend 05 §4: recordedAt は now-120s 以降だけ受理される。 */
export const LOCATION_MAX_AGE_MS = 120_000;
/** backend 05 §4: recordedAt は now+30s までだけ受理される。 */
export const LOCATION_MAX_FUTURE_MS = 30_000;
/** backend 02 §5: location バケットは 6 回/分。1 回あたり最短 10 秒 (60_000 / 6)。 */
export const LOCATION_MIN_UPLOAD_INTERVAL_MS = 10_000;
/** 前面での取得・送信の間隔。6/分の上限に十分収まる。 */
export const LOCATION_FOREGROUND_INTERVAL_MS = 30_000;
/** backend 05 §4 の source。native の LocationFix は種別を持たないので gps 固定。 */
export const LOCATION_SOURCE = "gps";

/** 送らない理由。null なら送ってよい。 */
export type LocationSkipReason = "mock" | "low_accuracy" | "stale" | "future";

/**
 * backend が拒否する fix を弾く (無条件に送らない)。isMock=true は backend が必ず拒否し、
 * 精度超過・古すぎ・未来すぎも 400 になるため、そもそも送らない。
 */
export function locationSkipReason(fix: LocationFix, nowMs: number): LocationSkipReason | null {
  if (fix.isMock) {
    return "mock";
  }
  if (!Number.isFinite(fix.accuracyM) || fix.accuracyM > LOCATION_MAX_ACCURACY_M) {
    return "low_accuracy";
  }
  const recordedAtMs = Date.parse(fix.recordedAt);
  if (!Number.isFinite(recordedAtMs)) {
    return "stale";
  }
  if (recordedAtMs < nowMs - LOCATION_MAX_AGE_MS) {
    return "stale";
  }
  if (recordedAtMs > nowMs + LOCATION_MAX_FUTURE_MS) {
    return "future";
  }
  return null;
}

/** 送らなかった理由の表示文。 */
export function describeSkipReason(reason: LocationSkipReason): string {
  switch (reason) {
    case "mock":
      return "疑似位置情報が有効なため、位置を送信できません。";
    case "low_accuracy":
      return "位置の精度が低いため送信していません。「正確な位置情報」を許可し、屋外などで再度お試しください。";
    case "stale":
    case "future":
      return "端末の位置情報の時刻が合っていないため送信できません。";
  }
}

/** 送ってよい fix か。 */
export function isLocationUploadable(fix: LocationFix, nowMs: number): boolean {
  return locationSkipReason(fix, nowMs) === null;
}

/**
 * 前回の送信から minIntervalMs 以上たったか。既定 10 秒で 6/分を超えない。
 * 未送信 (lastSentAtMs=0) は常に true。
 */
export function locationUploadDue(input: {
  lastSentAtMs: number;
  nowMs: number;
  minIntervalMs?: number;
}): boolean {
  if (input.lastSentAtMs <= 0) {
    return true;
  }
  const minIntervalMs = input.minIntervalMs ?? LOCATION_MIN_UPLOAD_INTERVAL_MS;
  return input.nowMs - input.lastSentAtMs >= minIntervalMs;
}

/** PUT /me/location の body (05 §4)。 */
export interface LocationUploadBody {
  lat: number;
  lon: number;
  accuracyM: number;
  recordedAt: string;
  source: string;
  isMock: boolean;
}

/** LocationFix を backend の body 形へ写す。 */
export function toLocationUploadBody(fix: LocationFix): LocationUploadBody {
  return {
    lat: fix.lat,
    lon: fix.lon,
    accuracyM: fix.accuracyM,
    recordedAt: fix.recordedAt,
    source: LOCATION_SOURCE,
    isMock: fix.isMock,
  };
}

const locationUploadListeners = new Set<() => void>();

/**
 * 位置の送信が成功したときに呼ばれる。403 location_stale で止まっている画面が、
 * 位置が届いた時点で取り直すために使う。戻り値で購読を外す。
 */
export function onLocationUploaded(listener: () => void): () => void {
  locationUploadListeners.add(listener);
  return () => {
    locationUploadListeners.delete(listener);
  };
}

/** データ層経由で現在位置を送る。204 なので戻り値は無い。 */
export async function uploadLocation(
  fix: LocationFix,
  opts: { signal?: AbortSignal } = {},
): Promise<void> {
  await putJson<void>("/me/location", toLocationUploadBody(fix), { signal: opts.signal });
  for (const listener of [...locationUploadListeners]) {
    listener();
  }
}

/** uploadLocation の差し替え (テスト・別トランスポート用)。 */
export type LocationSender = (
  fix: LocationFix,
  opts?: { signal?: AbortSignal },
) => Promise<void>;
