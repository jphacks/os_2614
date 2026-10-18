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

/**
 * 平滑化で想定する移動の速さ (m/s)。速歩き程度。これより速く動くと位置が遅れて付いてくるが、
 * 誤差の範囲を超えた移動は下のリセットで即座に追いつく。
 */
export const LOCATION_SMOOTHING_SPEED_MPS = 3;
/** 前の値からこれ以上あいたら平滑化せず、新しい値をそのまま使う。 */
export const LOCATION_SMOOTHING_RESET_MS = LOCATION_MAX_AGE_MS;
/** 前の値と新しい値の差が、2 つの誤差を合わせた標準偏差の何倍を超えたら移動とみなすか。 */
const LOCATION_SMOOTHING_JUMP_SIGMA = 3;

const EARTH_RADIUS_M = 6_371_000;

/** 近い 2 点の距離 (m)。平滑化の判定にしか使わないので正距円筒で近似する。 */
function approxDistanceM(a: LocationFix, b: LocationFix): number {
  const toRad = Math.PI / 180;
  const x = (b.lon - a.lon) * toRad * Math.cos(((a.lat + b.lat) / 2) * toRad);
  const y = (b.lat - a.lat) * toRad;
  return Math.hypot(x, y) * EARTH_RADIUS_M;
}

/**
 * 連続する fix を 1 次元の Kalman フィルタで平滑化する (緯度・経度を同じ重みで混ぜる)。
 * prev は前回の戻り値。accuracyM を分散 (accuracyM²) とみなし、経過時間ぶん
 * LOCATION_SMOOTHING_SPEED_MPS で不確かさを広げてから新しい値と精度で重み付けする。
 * 精度の悪い fix ほど効きが弱くなり、ビル反射などで一瞬だけ飛んだ値に引っ張られにくい。
 *
 * - 前回が無い / 古すぎる / モックの有無が変わった / 誤差の範囲を超えて動いた → next をそのまま返す
 * - next が prev より古い → prev を返す
 * - 戻り値の accuracyM は混ぜた後の推定誤差 (どちらの精度よりも小さくなる)
 */
export function smoothLocationFix(prev: LocationFix | null, next: LocationFix): LocationFix {
  if (prev === null || prev.isMock !== next.isMock) {
    return next;
  }
  if (!Number.isFinite(prev.accuracyM) || !Number.isFinite(next.accuracyM) || next.accuracyM <= 0) {
    return next;
  }
  const dtMs = Date.parse(next.recordedAt) - Date.parse(prev.recordedAt);
  if (!Number.isFinite(dtMs) || dtMs > LOCATION_SMOOTHING_RESET_MS) {
    return next;
  }
  if (dtMs < 0) {
    return prev;
  }
  const spreadM = (LOCATION_SMOOTHING_SPEED_MPS * dtMs) / 1_000;
  const prevVariance = prev.accuracyM ** 2 + spreadM ** 2;
  const nextVariance = next.accuracyM ** 2;
  if (approxDistanceM(prev, next) > LOCATION_SMOOTHING_JUMP_SIGMA * Math.sqrt(prevVariance + nextVariance)) {
    return next;
  }
  const gain = prevVariance / (prevVariance + nextVariance);
  return {
    lat: prev.lat + gain * (next.lat - prev.lat),
    lon: prev.lon + gain * (next.lon - prev.lon),
    accuracyM: Math.sqrt((prevVariance * nextVariance) / (prevVariance + nextVariance)),
    recordedAt: next.recordedAt,
    isMock: next.isMock,
  };
}

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
