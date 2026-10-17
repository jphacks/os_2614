// backend の実装待ちのため fetch はしない。代わりに、native が drain した
// Sighting と backend の token / 時計をつなぐ純関数と、送信前に sighting を
// 抱えておくための小さな outbox を置く。送信そのものは別途 (backend 実装後) 行う。

import type { BeaconToken, Sighting } from "./definitions";

const MS_PER_HOUR = 3_600_000;
const MS_PER_DAY = 24 * MS_PER_HOUR;

/** estimateClockOffset が使う直近サンプル数 (android README §10)。 */
const CLOCK_SAMPLE_LIMIT = 5;

/** backend 06 §2: lastSeenAt が 24 時間より前の項目は捨てる。 */
export const SIGHTING_MAX_AGE_MS = MS_PER_DAY;

/** backend 06 §2: 1 リクエスト 500 件まで。 */
const OUTBOX_DEFAULT_LIMIT = 500;

const DEFAULT_STORAGE_KEY = "chain.proximity.sighting-outbox";

/** token の窓が終わる時刻 (ms)。windowStart は backend の時計なので補正しない。 */
function windowEndMs(token: BeaconToken, windowSec: number): number {
  return Date.parse(token.windowStart) + windowSec * 1000;
}

/** nowMs を含む窓の token を返す。複数あれば最も新しい窓を選ぶ。なければ null。 */
export function selectCurrentToken(
  tokens: BeaconToken[],
  windowSec: number,
  nowMs: number,
): BeaconToken | null {
  let current: BeaconToken | null = null;
  let currentStart = Number.NEGATIVE_INFINITY;

  for (const token of tokens) {
    const start = Date.parse(token.windowStart);
    if (!Number.isFinite(start)) {
      continue;
    }
    if (start <= nowMs && nowMs < start + windowSec * 1000 && start > currentStart) {
      current = token;
      currentStart = start;
    }
  }

  return current;
}

/**
 * 手持ち token の残りが thresholdHours 未満なら true (backend 05 §2.2)。
 * token が空、または最後の窓が過ぎている場合も true。
 */
export function tokensNeedRefresh(
  tokens: BeaconToken[],
  windowSec: number,
  nowMs: number,
  thresholdHours = 6,
): boolean {
  if (tokens.length === 0) {
    return true;
  }

  let latestEnd = Number.NEGATIVE_INFINITY;
  for (const token of tokens) {
    const end = windowEndMs(token, windowSec);
    if (Number.isFinite(end)) {
      latestEnd = Math.max(latestEnd, end);
    }
  }

  if (!Number.isFinite(latestEnd)) {
    return true;
  }

  return latestEnd - nowMs < thresholdHours * MS_PER_HOUR;
}

/** backend の Date ヘッダと往復時間から 1 サンプル分のずれを測る材料。 */
export interface ClockSample {
  sentAtMs: number;
  receivedAtMs: number;
  serverDateMs: number;
}

function median(values: number[]): number {
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  if (sorted.length % 2 === 1) {
    return sorted[mid];
  }
  return (sorted[mid - 1] + sorted[mid]) / 2;
}

/**
 * backend の時計 - 端末の時計 (ms)。各サンプルの
 * serverDateMs - (sentAtMs + receivedAtMs) / 2 を求め、直近 5 件の中央値を返す。
 */
export function estimateClockOffset(samples: ClockSample[]): number {
  if (samples.length === 0) {
    return 0;
  }
  const offsets = samples
    .slice(-CLOCK_SAMPLE_LIMIT)
    .map((sample) => sample.serverDateMs - (sample.sentAtMs + sample.receivedAtMs) / 2);
  return median(offsets);
}

function earlierIso(a: string, b: string): string {
  const ta = Date.parse(a);
  const tb = Date.parse(b);
  if (!Number.isFinite(ta)) {
    return b;
  }
  if (!Number.isFinite(tb)) {
    return a;
  }
  return ta <= tb ? a : b;
}

function laterIso(a: string, b: string): string {
  const ta = Date.parse(a);
  const tb = Date.parse(b);
  if (!Number.isFinite(ta)) {
    return b;
  }
  if (!Number.isFinite(tb)) {
    return a;
  }
  return ta >= tb ? a : b;
}

function isSighting(value: unknown): value is Sighting {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const record = value as Record<string, unknown>;
  return (
    typeof record.token === "string" &&
    typeof record.firstSeenAt === "string" &&
    typeof record.lastSeenAt === "string" &&
    typeof record.maxRssi === "number" &&
    typeof record.count === "number"
  );
}

function hasLocalStorage(): boolean {
  return typeof localStorage !== "undefined";
}

/**
 * drainSightings で取り出した Sighting を、送信が成功するまで抱えておく。
 * - 同じ token は first=min / last=max / maxRssi=max / count=和 でマージする
 * - lastSeenAt が 24 時間より古いものは捨てる
 * - take(max) で送信対象を取り出し、ack(tokens) で成功分を消す
 * - localStorage に保存・復元する (失敗しても例外は投げない)
 */
export class SightingOutbox {
  private readonly storageKey: string;
  private readonly items = new Map<string, Sighting>();

  constructor(storageKey: string = DEFAULT_STORAGE_KEY) {
    this.storageKey = storageKey;
    this.restore();
  }

  get size(): number {
    return this.items.size;
  }

  add(items: Sighting[], nowMs: number = Date.now()): void {
    for (const item of items) {
      const existing = this.items.get(item.token);
      if (existing) {
        existing.firstSeenAt = earlierIso(existing.firstSeenAt, item.firstSeenAt);
        existing.lastSeenAt = laterIso(existing.lastSeenAt, item.lastSeenAt);
        existing.maxRssi = Math.max(existing.maxRssi, item.maxRssi);
        existing.count += item.count;
      } else {
        this.items.set(item.token, { ...item });
      }
    }
    this.prune(nowMs);
    this.persist();
  }

  /** 送信対象を最大 max 件返す。消すのは ack が成功した後 (at-least-once)。 */
  take(max: number = OUTBOX_DEFAULT_LIMIT, nowMs: number = Date.now()): Sighting[] {
    this.prune(nowMs);
    const result: Sighting[] = [];
    for (const item of this.items.values()) {
      if (result.length >= max) {
        break;
      }
      result.push({ ...item });
    }
    return result;
  }

  /** 送信が成功した token を outbox から消す。 */
  ack(tokens: string[]): void {
    if (tokens.length === 0) {
      return;
    }
    for (const token of tokens) {
      this.items.delete(token);
    }
    this.persist();
  }

  private prune(nowMs: number): void {
    const cutoff = nowMs - SIGHTING_MAX_AGE_MS;
    for (const [token, item] of this.items) {
      if (Date.parse(item.lastSeenAt) < cutoff) {
        this.items.delete(token);
      }
    }
  }

  private persist(): void {
    if (!hasLocalStorage()) {
      return;
    }
    try {
      localStorage.setItem(this.storageKey, JSON.stringify([...this.items.values()]));
    } catch {
      // 容量超過やプライベートモードでは保存できない。メモリ上だけで続ける。
    }
  }

  private restore(): void {
    if (!hasLocalStorage()) {
      return;
    }
    try {
      const raw = localStorage.getItem(this.storageKey);
      if (!raw) {
        return;
      }
      const parsed: unknown = JSON.parse(raw);
      if (!Array.isArray(parsed)) {
        return;
      }
      for (const value of parsed) {
        if (isSighting(value)) {
          this.items.set(value.token, { ...value });
        }
      }
    } catch {
      // 壊れた値は無視して空から始める。
    }
  }
}
