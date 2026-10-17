// beacon token の取得と ChainProximity への反映 (backend 05 §2.2)。
// - 取得はデータ層 (getJsonWithMeta) 経由。画面から直接 fetch しない。
// - 残りが 6 時間未満 (または未取得) なら取得し直し、それ以外は何もしない。
// - 応答の Date ヘッダから端末時計とのずれを測り setClockOffset に渡す。
// - token の値はログにも UI にも出さない (使い捨て ID を周囲に漏らさない)。

import { getJsonWithMeta } from "@/data/api";
import type { BeaconToken } from "./definitions";
import { estimateClockOffset, tokensNeedRefresh } from "./sync";

/** GET /beacon/tokens の応答 (controller.beaconTokensResponse、05 §2.2)。 */
export interface BeaconTokensResponse {
  windowSec: number;
  tokens: BeaconToken[];
}

/** token と時計の反映先。ChainProximity の該当 2 メソッドだけを要求する。 */
export interface BeaconSink {
  setBeaconTokens(opts: { windowSec: number; tokens: BeaconToken[] }): Promise<void>;
  setClockOffset(opts: { offsetMs: number }): Promise<void>;
}

/** 取得 1 回分。serverDateMs は Date ヘッダ (無ければ null)。往復時間の測定も含む。 */
export interface BeaconFetch extends BeaconTokensResponse {
  serverDateMs: number | null;
  sentAtMs: number;
  receivedAtMs: number;
}

export const BEACON_TOKEN_HOURS = 24;
export const TOKEN_REFRESH_THRESHOLD_HOURS = 6;

/** データ層経由で token を取得する。 */
export async function fetchBeaconTokens(
  opts: { hours?: number; signal?: AbortSignal } = {},
): Promise<BeaconFetch> {
  const hours = opts.hours ?? BEACON_TOKEN_HOURS;
  const sentAtMs = Date.now();
  const { data, serverDateMs } = await getJsonWithMeta<BeaconTokensResponse>("/beacon/tokens", {
    query: { hours },
    signal: opts.signal,
  });
  const receivedAtMs = Date.now();
  return {
    windowSec: data.windowSec,
    tokens: Array.isArray(data.tokens) ? data.tokens : [],
    serverDateMs,
    sentAtMs,
    receivedAtMs,
  };
}

export interface BeaconSyncResult {
  /** 取得し直したか。false なら手持ちをそのまま使った */
  refreshed: boolean;
  windowSec: number;
  tokens: BeaconToken[];
  /** setClockOffset に渡した値 (Date ヘッダが無ければ 0) */
  clockOffsetMs: number;
}

export interface BeaconSyncOptions {
  sink: BeaconSink;
  /** 手持ちの token。無ければ取得する */
  existing?: BeaconTokensResponse | null;
  hours?: number;
  signal?: AbortSignal;
  nowMs?: () => number;
  /** 取得の差し替え。テスト用。既定はデータ層の fetchBeaconTokens */
  fetchTokens?: (opts: { hours?: number; signal?: AbortSignal }) => Promise<BeaconFetch>;
}

/**
 * 手持ちの残りが 6 時間未満 (または未取得) なら再取得し、時計ずれと token を sink に反映する。
 * 残りが十分あれば取得も setClockOffset もしない。
 */
export async function syncBeaconTokens(opts: BeaconSyncOptions): Promise<BeaconSyncResult> {
  const nowMs = opts.nowMs ?? (() => Date.now());
  const existing = opts.existing ?? null;

  if (
    existing &&
    !tokensNeedRefresh(
      existing.tokens,
      existing.windowSec,
      nowMs(),
      TOKEN_REFRESH_THRESHOLD_HOURS,
    )
  ) {
    return {
      refreshed: false,
      windowSec: existing.windowSec,
      tokens: existing.tokens,
      clockOffsetMs: 0,
    };
  }

  const fetchTokens = opts.fetchTokens ?? fetchBeaconTokens;
  const fetched = await fetchTokens({ hours: opts.hours, signal: opts.signal });

  let clockOffsetMs = 0;
  if (fetched.serverDateMs !== null && Number.isFinite(fetched.serverDateMs)) {
    clockOffsetMs = estimateClockOffset([
      {
        sentAtMs: fetched.sentAtMs,
        receivedAtMs: fetched.receivedAtMs,
        serverDateMs: fetched.serverDateMs,
      },
    ]);
    await opts.sink.setClockOffset({ offsetMs: clockOffsetMs });
  }
  await opts.sink.setBeaconTokens({ windowSec: fetched.windowSec, tokens: fetched.tokens });

  return {
    refreshed: true,
    windowSec: fetched.windowSec,
    tokens: fetched.tokens,
    clockOffsetMs,
  };
}

/** 実機の ChainProximity に配線した版。画面・フックはこれを使う。 */
export async function syncBeaconTokensWithPlugin(
  opts: Omit<BeaconSyncOptions, "sink">,
): Promise<BeaconSyncResult> {
  const { ChainProximity } = await import("./index");
  return syncBeaconTokens({
    ...opts,
    sink: {
      setBeaconTokens: (next) => ChainProximity.setBeaconTokens(next),
      setClockOffset: (next) => ChainProximity.setClockOffset(next),
    },
  });
}
