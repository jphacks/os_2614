// WebSocket クライアント (specification/backend/10 §7)。
//
// 守っていること:
// - トークンを URL に載せない (アクセスログに残るため)。接続後に最初のメッセージとして
//   { type:"auth", token } を送る (10 §7.1:152)。
// - access JWT が切れる前に新しいトークンで auth を再送する (10 §7.1:159)。
// - 切断は指数バックオフで再接続し、購読 (sub) を再送する (10 §7.2)。
// - 1 接続あたりの購読は 50 Space まで (10 §7.2:172)。
// - ping/pong はブラウザの WebSocket 実装が RFC 6455 の制御フレームとして自動応答する。
//   サーバは 30 秒ごとに ping、10 秒以内に pong がなければ切断する (10 §7.1:160) が、
//   クライアント側では何も実装しなくてよい (ping/pong は JS に届かない)。
//
// プロトコルの判定・フレーム組み立て・バックオフ計算は純関数として切り出し、
// ブラウザを開かずに単体テストできるようにする。React への接続 (provider / event dispatch) は
// ここではやらない (todo 28/29)。

import { API_BASE, REFRESH_MARGIN_MS } from "@/auth/config";
import { session } from "@/auth/session";

/** サーバが auth を待つ時間 (10 §7.1)。この時間内に ready が来なければ再接続する。 */
export const DEFAULT_AUTH_TIMEOUT_MS = 5_000;
export const DEFAULT_BACKOFF_BASE_MS = 1_000;
export const DEFAULT_BACKOFF_MAX_MS = 30_000;
/** 1 接続あたりの購読上限 (10 §7.2:172)。 */
export const MAX_SUBSCRIPTIONS = 50;
/** JWT の有効期間が読めなかったときの再 auth 間隔。 */
export const REAUTH_FALLBACK_MS = 5 * 60_000;
/** 再 auth の最小間隔。ttl が margin より短い退化ケースでの無限ループを防ぐ。 */
export const MIN_REAUTH_MS = 1_000;

export type RealtimeState =
  | "idle"
  | "connecting"
  | "authing"
  | "ready"
  | "reconnecting"
  | "closed";

/** 切断の原因。manual と auth は再接続しない。network はバックオフで再接続する。 */
export type CloseCause = "manual" | "auth" | "network";

export interface ReadyFrame {
  type: "ready";
  did: string;
}
export interface SubscribedFrame {
  type: "subscribed";
  id: string;
  space: string;
}
export interface UnsubscribedFrame {
  type: "unsubscribed";
  space: string;
  reason?: string;
}
export interface ErrorFrame {
  type: "error";
  id?: string;
  code: string;
}
export interface EventFrame {
  type: "event";
  event: string;
  space?: string;
  data: unknown;
}

export type ServerFrame =
  | ReadyFrame
  | SubscribedFrame
  | UnsubscribedFrame
  | ErrorFrame
  | EventFrame;

// ---- 純関数 (プロトコル) ----

/** `https://` → `wss://`、`http://` → `ws://` に変換して `/api/v1/ws` を返す。 */
export function buildWsUrl(apiBase: string): string {
  const base = apiBase.replace(/\/+$/, "");
  const ws = base.replace(/^https:/i, "wss:").replace(/^http:/i, "ws:");
  return `${ws}/api/v1/ws`;
}

/** 接続後の最初のメッセージ (10 §7.1:155)。トークンはここにだけ載せる。 */
export function buildAuthFrame(token: string): string {
  return JSON.stringify({ type: "auth", token });
}

export function buildSubFrame(id: string, space: string): string {
  return JSON.stringify({ type: "sub", id, space });
}

export function buildUnsubFrame(space: string): string {
  return JSON.stringify({ type: "unsub", space });
}

/** サーバからのフレームを判別して返す。読めなければ null (未知の型も null)。 */
export function parseFrame(data: string): ServerFrame | null {
  try {
    const parsed: unknown = JSON.parse(data);
    if (typeof parsed !== "object" || parsed === null) {
      return null;
    }
    const r = parsed as Record<string, unknown>;
    if (typeof r.type !== "string") {
      return null;
    }
    switch (r.type) {
      case "ready":
        return typeof r.did === "string" ? { type: "ready", did: r.did } : null;
      case "subscribed":
        return typeof r.id === "string" && typeof r.space === "string"
          ? { type: "subscribed", id: r.id, space: r.space }
          : null;
      case "unsubscribed":
        return typeof r.space === "string"
          ? {
              type: "unsubscribed",
              space: r.space,
              reason: typeof r.reason === "string" ? r.reason : undefined,
            }
          : null;
      case "error":
        return typeof r.code === "string"
          ? { type: "error", id: typeof r.id === "string" ? r.id : undefined, code: r.code }
          : null;
      case "event":
        return typeof r.event === "string"
          ? {
              type: "event",
              event: r.event,
              space: typeof r.space === "string" ? r.space : undefined,
              data: r.data,
            }
          : null;
      default:
        return null;
    }
  } catch {
    return null;
  }
}

/** auth が拒否された (再送しても直らない) エラーか。 */
export function isAuthError(code: string): boolean {
  return code === "unauthenticated";
}

/**
 * 指数バックオフ。attempt 回目の再接続までの待ち時間 (ms)。
 * base * 2^attempt を max で打ち切り、jitter で 0.5〜1.0 倍に揺らす (再接続の雪崩対策)。
 * random は 0〜1 の一様乱数。テストでは固定値を渡して決定的にできる。
 */
export function computeBackoff(
  attempt: number,
  baseMs: number,
  maxMs: number,
  random: () => number = Math.random,
): number {
  const exp = Math.min(baseMs * 2 ** attempt, maxMs);
  return Math.floor(exp * (0.5 + random() / 2));
}

/** JWT の exp - iat (ms)。読めなければ null。署名は検証しない (期限の見積もりにだけ使う)。 */
export function jwtTtlMs(jwt: string): number | null {
  try {
    const payload = jwt.split(".")[1];
    const json = atob(payload.replace(/-/g, "+").replace(/_/g, "/"));
    const { iat, exp } = JSON.parse(json) as { iat?: unknown; exp?: unknown };
    if (typeof iat === "number" && typeof exp === "number" && exp > iat) {
      return (exp - iat) * 1000;
    }
  } catch {
    // 下で null を返す。
  }
  return null;
}

/** 再 auth までの待ち時間。ttl が読めなければ fallback、margin を切る分だけ早める。 */
export function computeReauthDelayMs(ttlMs: number | null, marginMs: number): number {
  if (ttlMs === null) {
    return REAUTH_FALLBACK_MS;
  }
  return Math.max(ttlMs - marginMs, MIN_REAUTH_MS);
}

// ---- トランスポート抽象 (テストで差し替えられるようにする) ----

export interface WebSocketLike {
  send(data: string): void;
  close(): void;
  onopen: (() => void) | null;
  onmessage: ((event: { data: unknown }) => void) | null;
  onclose: ((event: { code?: number }) => void) | null;
  onerror: (() => void) | null;
}

export type WebSocketFactory = (url: string) => WebSocketLike;

function defaultWebSocketFactory(url: string): WebSocketLike {
  return new WebSocket(url) as unknown as WebSocketLike;
}

export interface RealtimeClientOptions {
  apiBase?: string;
  getToken?: () => Promise<string>;
  factory?: WebSocketFactory;
  authTimeoutMs?: number;
  backoffBaseMs?: number;
  backoffMaxMs?: number;
  maxSubscriptions?: number;
  reauthMarginMs?: number;
}

export interface RealtimeEvents {
  state: RealtimeState;
  ready: { did: string };
  event: EventFrame;
  subscribed: { id: string; space: string };
  unsubscribed: { space: string; reason?: string };
  error: { id?: string; space?: string; code: string };
  close: { cause: CloseCause };
}

type Listener = (payload: unknown) => void;

/**
 * 1 本の WebSocket 接続と、その上の購読・再接続を管理するクライアント。
 * React には依存しない。provider (todo 29) から 1 インスタンス使う想定。
 */
export class RealtimeClient {
  private readonly apiBase: string;
  private readonly getToken: () => Promise<string>;
  private readonly factory: WebSocketFactory;
  private readonly authTimeoutMs: number;
  private readonly backoffBaseMs: number;
  private readonly backoffMaxMs: number;
  private readonly maxSubscriptions: number;
  private readonly reauthMarginMs: number;

  private ws: WebSocketLike | null = null;
  private stateValue: RealtimeState = "idle";
  private did: string | null = null;
  private attempt = 0;
  private closedByUser = false;
  private authFailed = false;
  private idCounter = 0;
  private currentToken: string | null = null;

  private authTimer: ReturnType<typeof setTimeout> | null = null;
  private reauthTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

  /** space -> その接続で使う sub の id。再接続時は id を振り直す。 */
  private readonly subscriptions = new Map<string, string>();
  private readonly listeners = new Map<keyof RealtimeEvents, Set<Listener>>();

  constructor(opts: RealtimeClientOptions = {}) {
    this.apiBase = opts.apiBase ?? API_BASE;
    this.getToken = opts.getToken ?? (() => session.getAccessToken());
    this.factory = opts.factory ?? defaultWebSocketFactory;
    this.authTimeoutMs = opts.authTimeoutMs ?? DEFAULT_AUTH_TIMEOUT_MS;
    this.backoffBaseMs = opts.backoffBaseMs ?? DEFAULT_BACKOFF_BASE_MS;
    this.backoffMaxMs = opts.backoffMaxMs ?? DEFAULT_BACKOFF_MAX_MS;
    this.maxSubscriptions = opts.maxSubscriptions ?? MAX_SUBSCRIPTIONS;
    this.reauthMarginMs = opts.reauthMarginMs ?? REFRESH_MARGIN_MS;
  }

  get state(): RealtimeState {
    return this.stateValue;
  }

  get subscribedSpaces(): string[] {
    return Array.from(this.subscriptions.keys());
  }

  on<K extends keyof RealtimeEvents>(
    event: K,
    handler: (payload: RealtimeEvents[K]) => void,
  ): () => void {
    let set = this.listeners.get(event);
    if (!set) {
      set = new Set<Listener>();
      this.listeners.set(event, set);
    }
    const wrapped: Listener = (payload) => handler(payload as RealtimeEvents[K]);
    set.add(wrapped);
    return () => set.delete(wrapped);
  }

  /** 接続を開始する。既に接続中・再接続待ちなら何もしない。終端 (closed) からは復帰しない。 */
  connect(): void {
    if (this.closedByUser || this.authFailed) {
      return;
    }
    if (this.ws !== null || this.reconnectTimer !== null) {
      return;
    }
    this.setState("connecting");
    this.open();
  }

  /** 明示的に閉じる。以降は再接続しない。 */
  close(): void {
    this.closedByUser = true;
    this.clearTimers();
    const ws = this.ws;
    this.ws = null;
    if (ws) {
      ws.onopen = null;
      ws.onmessage = null;
      ws.onclose = null;
      ws.onerror = null;
      ws.close();
    }
    this.setState("closed");
    this.emit("close", { cause: "manual" });
  }

  /** space を購読する。既に購読済みなら既存の id を返す。上限を超えると throw。 */
  subscribe(space: string): string {
    const existing = this.subscriptions.get(space);
    if (existing !== undefined) {
      return existing;
    }
    if (this.subscriptions.size >= this.maxSubscriptions) {
      throw new Error(`subscription limit reached (${this.maxSubscriptions})`);
    }
    const id = this.nextId();
    this.subscriptions.set(space, id);
    if (this.stateValue === "ready" && this.ws !== null) {
      this.send(buildSubFrame(id, space));
    }
    return id;
  }

  unsubscribe(space: string): void {
    if (!this.subscriptions.delete(space)) {
      return;
    }
    if (this.stateValue === "ready" && this.ws !== null) {
      this.send(buildUnsubFrame(space));
    }
  }

  // ---- 内部: 接続 ----

  private open(): void {
    if (this.closedByUser || this.authFailed) {
      return;
    }
    const url = buildWsUrl(this.apiBase);
    const ws = this.factory(url);
    this.ws = ws;
    ws.onopen = () => void this.onOpen();
    ws.onmessage = (event) => this.onMessage(event.data);
    ws.onclose = () => this.onClose();
    // onerror の後には必ず onclose が来る (RFC 6455)。再接続は onClose で扱う。
    this.startAuthTimeout();
  }

  private async onOpen(): Promise<void> {
    this.setState("authing");
    try {
      const token = await this.getToken();
      if (this.ws === null || this.stateValue !== "authing") {
        return;
      }
      this.currentToken = token;
      this.send(buildAuthFrame(token));
    } catch {
      // トークンが取れない (未ログイン等)。再接続しても直らないので終端にする。
      this.authFailed = true;
      this.clearTimers();
      const ws = this.ws;
      this.ws = null;
      if (ws) {
        ws.onclose = null;
        ws.close();
      }
      this.setState("closed");
      this.emit("close", { cause: "auth" });
    }
  }

  private onMessage(data: unknown): void {
    const frame = parseFrame(typeof data === "string" ? data : String(data));
    if (frame === null) {
      return;
    }
    switch (frame.type) {
      case "ready":
        this.onReady(frame);
        break;
      case "subscribed":
        this.emit("subscribed", frame);
        break;
      case "unsubscribed":
        this.subscriptions.delete(frame.space);
        this.emit("unsubscribed", frame);
        break;
      case "error":
        this.onError(frame);
        break;
      case "event":
        this.emit("event", frame);
        break;
    }
  }

  private onReady(frame: ReadyFrame): void {
    this.clearAuthTimer();
    this.attempt = 0;
    this.did = frame.did;
    this.setState("ready");
    this.emit("ready", { did: frame.did });
    // 再接続で失われた購読を再送する (id は新しい接続用に振り直す)。
    for (const space of this.subscriptions.keys()) {
      this.resubscribe(space);
    }
    if (this.currentToken !== null) {
      this.scheduleReauth(this.currentToken);
    }
  }

  private onError(frame: ErrorFrame): void {
    // auth 失敗 (id なし)。サーバがこの後に切断するので、終端にして再接続しない。
    if (frame.id === undefined && isAuthError(frame.code)) {
      this.authFailed = true;
      this.emit("error", { code: frame.code });
      return;
    }
    // 購読エラー (id 付き)。該当 space を外して通知する。
    const space = frame.id !== undefined ? this.spaceById(frame.id) : undefined;
    if (space !== undefined) {
      this.subscriptions.delete(space);
    }
    this.emit("error", { id: frame.id, space, code: frame.code });
  }

  private onClose(): void {
    const ws = this.ws;
    this.ws = null;
    this.clearTimers();
    if (ws === null) {
      return;
    }
    if (this.closedByUser) {
      return;
    }
    if (this.authFailed) {
      this.setState("closed");
      this.emit("close", { cause: "auth" });
      return;
    }
    this.scheduleReconnect();
  }

  private scheduleReconnect(): void {
    if (this.closedByUser || this.authFailed || this.reconnectTimer !== null) {
      return;
    }
    const delay = computeBackoff(this.attempt, this.backoffBaseMs, this.backoffMaxMs);
    this.attempt += 1;
    this.setState("reconnecting");
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.open();
    }, delay);
  }

  private resubscribe(space: string): void {
    const id = this.nextId();
    this.subscriptions.set(space, id);
    this.send(buildSubFrame(id, space));
  }

  private scheduleReauth(token: string): void {
    this.clearReauthTimer();
    const delay = computeReauthDelayMs(jwtTtlMs(token), this.reauthMarginMs);
    this.reauthTimer = setTimeout(() => {
      this.reauthTimer = null;
      void this.reauth();
    }, delay);
  }

  private async reauth(): Promise<void> {
    if (this.ws === null || this.stateValue !== "ready") {
      return;
    }
    try {
      const token = await this.getToken();
      if (this.ws === null || this.stateValue !== "ready") {
        return;
      }
      this.currentToken = token;
      this.send(buildAuthFrame(token));
      this.scheduleReauth(token);
    } catch {
      // 未ログイン等。次の切断で onClose が終端にする。
    }
  }

  // ---- 内部: 小物 ----

  private nextId(): string {
    this.idCounter += 1;
    return `c${this.idCounter}`;
  }

  private spaceById(id: string): string | undefined {
    for (const [space, subId] of this.subscriptions) {
      if (subId === id) {
        return space;
      }
    }
    return undefined;
  }

  private send(data: string): void {
    this.ws?.send(data);
  }

  private startAuthTimeout(): void {
    this.clearAuthTimer();
    this.authTimer = setTimeout(() => {
      this.authTimer = null;
      if (this.stateValue === "connecting" || this.stateValue === "authing") {
        this.dropAndReconnect();
      }
    }, this.authTimeoutMs);
  }

  private dropAndReconnect(): void {
    const ws = this.ws;
    this.ws = null;
    if (ws) {
      ws.onclose = null;
      ws.close();
    }
    this.scheduleReconnect();
  }

  private clearTimers(): void {
    this.clearAuthTimer();
    this.clearReauthTimer();
    this.clearReconnectTimer();
  }

  private clearAuthTimer(): void {
    if (this.authTimer !== null) {
      clearTimeout(this.authTimer);
      this.authTimer = null;
    }
  }

  private clearReauthTimer(): void {
    if (this.reauthTimer !== null) {
      clearTimeout(this.reauthTimer);
      this.reauthTimer = null;
    }
  }

  private clearReconnectTimer(): void {
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  private setState(next: RealtimeState): void {
    if (this.stateValue === next) {
      return;
    }
    this.stateValue = next;
    this.emit("state", next);
  }

  private emit<K extends keyof RealtimeEvents>(event: K, payload: RealtimeEvents[K]): void {
    const set = this.listeners.get(event);
    if (!set) {
      return;
    }
    for (const listener of set) {
      listener(payload);
    }
  }
}
