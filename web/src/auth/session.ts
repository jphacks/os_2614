// アプリセッション (specification/backend/03 §3) の持ち主。
//
// 守っていること:
// - refresh token はローテーションされ、使用済みの token を再提示すると backend は系列ごと失効させる。
//   そのため refresh はタブ内では 1 本の Promise に、タブ間では Web Locks で直列化する。
//   ロックを取った後に保存値を読み直し、他のタブが先に更新していればそれを使う。
// - access token の期限は端末の時計を信用せず、JWT の exp - iat (有効期間) を受信時刻に足して求める。
// - one-time code は 1 回しか使えないので、同じ code の交換は 1 本にまとめる (StrictMode の二重実行対策)。
//
// specification/android §3 は native がトークンを持つ前提だが、現在の ChainProximity は端末機能だけを持ち、
// backend 通信は JS 側が担う (src/device/proximity/definitions.ts)。それに合わせてここで持つ。
// 保存先は localStorage。native の暗号化ストレージに移すときは TokenStore だけを差し替える。

import { Capacitor } from "@capacitor/core";
import { API_BASE, REFRESH_MARGIN_MS } from "./config";
import { ApiError, SignedOutError, toApiError } from "./errors";

/** POST /api/v1/auth/exchange と /auth/refresh の応答 (backend/internal/service/auth.go Tokens)。 */
interface TokensResponse {
  did: string;
  accessToken: string;
  accessExpiresAt: string;
  refreshToken: string;
}

interface StoredSession {
  did: string;
  accessToken: string;
  refreshToken: string;
  /** 端末の時計での access token の期限 (ms) */
  accessExpiresAtMs: number;
}

export type SignedOutReason = "none" | "expired" | "loggedOut";

export type AuthState =
  | { status: "loading" }
  | { status: "signedOut"; reason: SignedOutReason }
  | { status: "signedIn"; did: string };

const STORAGE_KEY = "chain.auth.session.v1";
const LOCK_NAME = "chain.auth.refresh";
const LOADING: AuthState = { status: "loading" };

interface TokenStore {
  read(): StoredSession | null;
  write(session: StoredSession | null): void;
  /** 他のタブが書き換えたときに呼ばれる。戻り値で購読を解除する */
  watch(onChange: () => void): () => void;
}

function isStoredSession(v: unknown): v is StoredSession {
  if (typeof v !== "object" || v === null) {
    return false;
  }
  const r = v as Record<string, unknown>;
  return (
    typeof r.did === "string" &&
    typeof r.accessToken === "string" &&
    typeof r.refreshToken === "string" &&
    typeof r.accessExpiresAtMs === "number"
  );
}

const localTokenStore: TokenStore = {
  read() {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (!raw) {
        return null;
      }
      const parsed: unknown = JSON.parse(raw);
      return isStoredSession(parsed) ? parsed : null;
    } catch {
      return null;
    }
  },
  write(session) {
    try {
      if (session) {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(session));
      } else {
        localStorage.removeItem(STORAGE_KEY);
      }
    } catch {
      // 保存できなくてもメモリ上のセッションで続ける。次回起動時はログインし直しになる。
    }
  },
  watch(onChange) {
    const handler = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY || e.key === null) {
        onChange();
      }
    };
    window.addEventListener("storage", handler);
    return () => window.removeEventListener("storage", handler);
  },
};

/** JWT の exp - iat (ms)。読めなければ null。署名は検証しない (期限の見積もりにだけ使う)。 */
function accessTtlMs(jwt: string): number | null {
  try {
    const payload = jwt.split(".")[1];
    const json = atob(payload.replace(/-/g, "+").replace(/_/g, "/"));
    const { iat, exp } = JSON.parse(json) as { iat?: unknown; exp?: unknown };
    if (typeof iat === "number" && typeof exp === "number" && exp > iat) {
      return (exp - iat) * 1000;
    }
  } catch {
    // 下で accessExpiresAt にフォールバックする。
  }
  return null;
}

/** sentAtMs は要求を送った時刻。受信時刻より前を起点にして、期限を短めに見積もる。 */
function toStored(t: TokensResponse, sentAtMs: number): StoredSession {
  const ttl = accessTtlMs(t.accessToken);
  return {
    did: t.did,
    accessToken: t.accessToken,
    refreshToken: t.refreshToken,
    accessExpiresAtMs: ttl !== null ? sentAtMs + ttl : Date.parse(t.accessExpiresAt),
  };
}

function isFresh(s: StoredSession): boolean {
  return s.accessExpiresAtMs - Date.now() > REFRESH_MARGIN_MS;
}

function withRefreshLock<T>(fn: () => Promise<T>): Promise<T> {
  if (typeof navigator !== "undefined" && navigator.locks) {
    return navigator.locks.request(LOCK_NAME, fn) as Promise<T>;
  }
  return fn();
}

function deviceName(): string {
  return Capacitor.getPlatform() === "android" ? "Android" : "Web browser";
}

async function postJson(path: string, body: unknown, accessToken?: string): Promise<Response> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (accessToken) {
    headers.Authorization = `Bearer ${accessToken}`;
  }
  return fetch(`${API_BASE}${path}`, { method: "POST", headers, body: JSON.stringify(body) });
}

export class SessionManager {
  private session: StoredSession | null = null;
  private state: AuthState = LOADING;
  private initialized = false;
  private refreshing: Promise<StoredSession> | null = null;
  private readonly exchanges = new Map<string, Promise<void>>();
  private readonly listeners = new Set<() => void>();

  constructor(private readonly store: TokenStore) {}

  /**
   * 保存値を読み込む。公開メソッドの入口で呼ぶ (子の effect は親より先に走るので、Provider の effect では遅い)。
   * prerender では window がないので、ブラウザでしか通らない経路からだけ呼ぶ。
   */
  private init(): void {
    if (this.initialized) {
      return;
    }
    this.initialized = true;
    this.adopt(this.store.read(), "none");
    this.store.watch(() => this.adopt(this.store.read(), "none"));
  }

  // ---- useSyncExternalStore 用 ----

  subscribe = (listener: () => void): (() => void) => {
    this.init();
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getState = (): AuthState => this.state;

  getServerState = (): AuthState => LOADING;

  // ---- ログイン / ログアウト ----

  /** deep link で受けた one-time code を交換する。同じ code なら同じ Promise を返す。 */
  completeLogin(code: string): Promise<void> {
    this.init();
    let pending = this.exchanges.get(code);
    if (!pending) {
      pending = this.exchange(code).catch((e: unknown) => {
        // backend に届かなかったときだけ再試行できるようにする。届いた code は使用済み。
        if (!(e instanceof ApiError)) {
          this.exchanges.delete(code);
        }
        throw e;
      });
      this.exchanges.set(code, pending);
    }
    return pending;
  }

  /** backend のセッションを失効させてから手元を消す。backend に届かなくても手元は消す。 */
  async logout(): Promise<void> {
    this.init();
    try {
      if (this.session) {
        const token = await this.getAccessToken();
        await postJson("/api/v1/auth/logout", {}, token);
      }
    } catch {
      // 失効させられなかったセッションは 30 日で切れる。
    } finally {
      this.signOut("loggedOut");
    }
  }

  // ---- API 呼び出し用 ----

  /** 期限まで REFRESH_MARGIN_MS を切っていれば refresh してから返す。未ログインなら SignedOutError。 */
  async getAccessToken(): Promise<string> {
    this.init();
    const s = this.session;
    if (!s) {
      throw new SignedOutError();
    }
    if (isFresh(s)) {
      return s.accessToken;
    }
    return (await this.refresh(s.accessToken)).accessToken;
  }

  /**
   * used で API を呼んで 401 unauthenticated が返ったときに呼ぶ。
   * 既に新しい token があればそれを、なければ refresh して返す。refresh も 401 なら SignedOutError。
   */
  async recoverFromUnauthorized(used: string): Promise<string> {
    return (await this.refresh(used)).accessToken;
  }

  // ---- 内部 ----

  private async exchange(code: string): Promise<void> {
    const sentAt = Date.now();
    const res = await postJson("/api/v1/auth/exchange", { code, deviceName: deviceName() });
    if (!res.ok) {
      throw await toApiError(res);
    }
    this.save(toStored((await res.json()) as TokensResponse, sentAt));
  }

  /** stale は手元で古くなった (または 401 を受けた) access token。 */
  private refresh(stale: string): Promise<StoredSession> {
    this.refreshing ??= withRefreshLock(async () => {
      // ロック待ちの間に他のタブが更新・ログアウトしているかもしれないので読み直す。
      const latest = this.store.read() ?? this.session;
      if (!latest) {
        this.signOut("expired");
        throw new SignedOutError();
      }
      if (latest.accessToken !== stale && isFresh(latest)) {
        this.adopt(latest, "none");
        return latest;
      }

      const sentAt = Date.now();
      const res = await postJson("/api/v1/auth/refresh", { refreshToken: latest.refreshToken });
      if (res.status === 401) {
        this.signOut("expired");
        throw new SignedOutError();
      }
      if (!res.ok) {
        // 5xx やネットワーク断ではトークンを残す。次の呼び出しで再試行する。
        throw await toApiError(res);
      }
      const next = toStored((await res.json()) as TokensResponse, sentAt);
      this.save(next);
      return next;
    }).finally(() => {
      this.refreshing = null;
    });
    return this.refreshing;
  }

  private save(session: StoredSession): void {
    this.store.write(session);
    this.adopt(session, "none");
  }

  private signOut(reason: SignedOutReason): void {
    this.store.write(null);
    this.adopt(null, reason);
  }

  private adopt(session: StoredSession | null, reason: SignedOutReason): void {
    this.session = session;
    const prev = this.state;
    let next: AuthState;
    if (session) {
      next =
        prev.status === "signedIn" && prev.did === session.did
          ? prev
          : { status: "signedIn", did: session.did };
    } else {
      next = prev.status === "signedOut" && prev.reason === reason ? prev : { status: "signedOut", reason };
    }
    if (next !== prev) {
      this.state = next;
      for (const l of this.listeners) {
        l();
      }
    }
  }
}

export const session = new SessionManager(localTokenStore);
