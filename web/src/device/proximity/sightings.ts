// sighting の送信 (backend 06 §2)。native が drain した観測を SightingOutbox に積み、
// 500 件ずつ POST /encounters/sightings して成功分を ack する at-least-once。
//
// - 送信はデータ層 (postJson) 経由。画面から直接 fetch しない。
// - 応答は {accepted, dropped} だけで、どの token が成立したかは返らない。dropped は
//   「token 解決失敗 / RSSI が弱い / 24h 超」など backend が判断した破棄なので、再送しても
//   成立しない。よって HTTP 成功 (= リクエスト受理) をもって送った分をまとめて ack する。
// - 429 は Retry-After に従ってバックオフし、retryAfterMs を呼び出し側に返す (再送は呼び出し側)。
// - 24h より前の lastSeenAt は SightingOutbox.take が prune するため送られない。

import { postJson } from "@/data/api";
import { ApiError } from "@/auth/errors";
import type { Sighting } from "./definitions";
import type { SightingOutbox } from "./sync";

/** backend 06 §2: 1 リクエスト 500 件まで。 */
export const SIGHTING_BATCH_LIMIT = 500;

/** backend 06 §2: FGS 稼働中は 5 分ごとに送る。 */
export const SIGHTING_UPLOAD_INTERVAL_MS = 5 * 60_000;

/** backend 06 §2: 未送信がこれを超えたら送る。 */
export const SIGHTING_PENDING_THRESHOLD = 50;

/** Retry-After の無い 429 で使う待ち時間。sightings バケットは 1 分窓 (02 §5)。 */
export const SIGHTING_RATE_LIMIT_FALLBACK_MS = 60_000;

/** 1 回の flush で送る最大リクエスト数。暴走ループを止めるだけの安全弁。 */
const DEFAULT_MAX_BATCHES = 12;

/** POST /encounters/sightings の応答 (06 §2)。誰と成立したかは返らない。 */
export interface SightingsResponse {
  accepted: number;
  dropped: number;
}

/** native の sighting バッファ。ChainProximity.drainSightings と同じ形。 */
export interface SightingSource {
  drainSightings(): Promise<{ items: Sighting[] }>;
}

/** 送信を始めた理由 (06 §2)。foreground は前面復帰のイベントで表す。 */
export type SightingFlushReason = "interval" | "pending" | "foreground";

/** flush がどう終わったか。 */
export type SightingFlushStopped = "empty" | "rate_limited" | "error" | "max_batches";

export type SightingSend = (items: Sighting[]) => Promise<SightingsResponse>;

export interface SightingFlushResult {
  /** POST して ack した件数 */
  sent: number;
  /** 成功した POST の回数 */
  batches: number;
  /** 送信後に outbox に残った件数 (失敗分はここに残る) */
  pending: number;
  /** 429 のときの待ち時間 (ms)。Retry-After が無ければフォールバック。それ以外は null */
  retryAfterMs: number | null;
  stopped: SightingFlushStopped;
  /** 失敗した原因。成功 / 空なら null */
  error: unknown;
}

export interface SightingFlushOptions {
  outbox: SightingOutbox;
  nowMs?: () => number;
  /** 1 リクエストの上限。既定 500 */
  batchLimit?: number;
  /** 1 回の flush で送る最大リクエスト数。既定 12 */
  maxBatches?: number;
  signal?: AbortSignal;
  /** 送信の差し替え。テスト用。既定はデータ層の postJson */
  send?: SightingSend;
}

function defaultSend(items: Sighting[], signal?: AbortSignal): Promise<SightingsResponse> {
  return postJson<SightingsResponse>("/encounters/sightings", { items }, { signal });
}

/**
 * outbox を 500 件ずつ送り、成功した分を ack する。outbox が空になるか、
 * maxBatches に達するか、429 / それ以外の失敗まで続ける。失敗分は outbox に残る。
 */
export async function flushSightings(opts: SightingFlushOptions): Promise<SightingFlushResult> {
  const { outbox } = opts;
  const nowMs = opts.nowMs ?? (() => Date.now());
  const batchLimit = opts.batchLimit ?? SIGHTING_BATCH_LIMIT;
  const maxBatches = opts.maxBatches ?? DEFAULT_MAX_BATCHES;
  const send = opts.send ?? ((items: Sighting[]) => defaultSend(items, opts.signal));

  let sent = 0;
  let batches = 0;
  let retryAfterMs: number | null = null;
  let stopped: SightingFlushStopped = "empty";
  let error: unknown = null;

  for (;;) {
    const items = outbox.take(batchLimit, nowMs());
    if (items.length === 0) {
      stopped = "empty";
      break;
    }

    try {
      await send(items);
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 429) {
        retryAfterMs = cause.retryAfterMs ?? SIGHTING_RATE_LIMIT_FALLBACK_MS;
        stopped = "rate_limited";
      } else {
        stopped = "error";
      }
      error = cause;
      break;
    }

    outbox.ack(items.map((item) => item.token));
    sent += items.length;
    batches += 1;

    if (batches >= maxBatches) {
      stopped = "max_batches";
      break;
    }
  }

  return { sent, batches, pending: outbox.size, retryAfterMs, stopped, error };
}

/**
 * native が drain した sighting を outbox に積む。native 側は drain で消えるので、
 * ここで受けないと失われる (at-least-once の起点)。積んだ件数を返す。
 */
export async function drainIntoSightingsOutbox(
  source: SightingSource,
  outbox: SightingOutbox,
  nowMs: number = Date.now(),
): Promise<number> {
  const { items } = await source.drainSightings();
  if (items.length === 0) {
    return 0;
  }
  outbox.add(items, nowMs);
  return items.length;
}

export interface SightingTriggerInput {
  /** outbox に残っている未送信件数 */
  pending: number;
  /** すれ違いモード (Foreground Service) が動いているか */
  running: boolean;
  nowMs: number;
  /** 最後に送信を試みた時刻 (ms)。未送信なら 0 */
  lastFlushAtMs: number;
  /** 前面復帰のイベントで呼んだか */
  foreground?: boolean;
  /** 429 バックオフの解除時刻。この時刻まで送らない */
  rateLimitedUntilMs?: number;
}

/**
 * いま送るべきかを返す純関数 (06 §2)。送る理由が無ければ null。
 * 優先順は バックオフ → 前面復帰 → 50 件超 → 5 分ごと。24h 分の prune は outbox 側。
 */
export function sightingFlushReason(input: SightingTriggerInput): SightingFlushReason | null {
  if (input.pending <= 0) {
    return null;
  }
  if (input.rateLimitedUntilMs !== undefined && input.nowMs < input.rateLimitedUntilMs) {
    return null;
  }
  if (input.foreground === true) {
    return "foreground";
  }
  if (input.pending > SIGHTING_PENDING_THRESHOLD) {
    return "pending";
  }
  if (input.running && input.nowMs - input.lastFlushAtMs >= SIGHTING_UPLOAD_INTERVAL_MS) {
    return "interval";
  }
  return null;
}

export interface SightingSyncOptions {
  outbox: SightingOutbox;
  /** native の drain 元。省略すると drain せず、outbox の既存分だけ送る */
  source?: SightingSource;
  nowMs?: () => number;
  running?: boolean;
  foreground?: boolean;
  lastFlushAtMs?: number;
  rateLimitedUntilMs?: number;
  batchLimit?: number;
  maxBatches?: number;
  send?: SightingSend;
  signal?: AbortSignal;
}

export interface SightingSyncResult {
  /** native から取り込んだ件数 */
  drained: number;
  /** 送った理由。送らなかったら null */
  reason: SightingFlushReason | null;
  /** 送った結果。送らなかったら null */
  flush: SightingFlushResult | null;
}

/**
 * 1 回分の同期: native を outbox に drain し、trigger に当てはまれば送る。
 * lifecycle (5 分 timer / pending 監視 / 前面復帰) は呼び出し側が回す (todo 26)。
 */
export async function syncSightings(opts: SightingSyncOptions): Promise<SightingSyncResult> {
  const nowMs = opts.nowMs ?? (() => Date.now());

  let drained = 0;
  if (opts.source) {
    drained = await drainIntoSightingsOutbox(opts.source, opts.outbox, nowMs());
  }

  const reason = sightingFlushReason({
    pending: opts.outbox.size,
    running: opts.running ?? false,
    nowMs: nowMs(),
    lastFlushAtMs: opts.lastFlushAtMs ?? 0,
    foreground: opts.foreground,
    rateLimitedUntilMs: opts.rateLimitedUntilMs,
  });

  if (reason === null) {
    return { drained, reason: null, flush: null };
  }

  const flush = await flushSightings({
    outbox: opts.outbox,
    nowMs,
    batchLimit: opts.batchLimit,
    maxBatches: opts.maxBatches,
    send: opts.send,
    signal: opts.signal,
  });
  return { drained, reason, flush };
}

/** 実機の ChainProximity を drain 元にした版。todo 26 が起動する。 */
export async function syncSightingsWithPlugin(
  opts: Omit<SightingSyncOptions, "source">,
): Promise<SightingSyncResult> {
  const { ChainProximity } = await import("./index");
  return syncSightings({
    ...opts,
    source: { drainSightings: () => ChainProximity.drainSightings() },
  });
}
