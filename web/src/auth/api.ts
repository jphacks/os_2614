// 認証付きで /api/v1 を呼ぶ。401 unauthenticated なら 1 回だけ refresh して再送する。
// atproto_session_expired (PDS 側のセッション切れ) は refresh では直らないので、そのまま ApiError で返す。

import { API_BASE } from "./config";
import { toApiError } from "./errors";
import { session } from "./session";

export interface ApiRequest {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  /** JSON にして送る */
  body?: unknown;
  query?: Record<string, string | number | boolean | undefined>;
  signal?: AbortSignal;
}

function buildUrl(path: string, query: ApiRequest["query"]): string {
  const url = new URL(`${API_BASE}/api/v1${path}`);
  for (const [k, v] of Object.entries(query ?? {})) {
    if (v !== undefined) {
      url.searchParams.set(k, String(v));
    }
  }
  return url.toString();
}

/** apiWithMeta の戻り値。応答の Date ヘッダも添える (端末時計のずれ測定用)。 */
export interface ApiMeta<T> {
  data: T;
  /** 応答の Date ヘッダ (ms)。無い / 不正なら null。 */
  serverDateMs: number | null;
}

/** 応答の Date ヘッダを ms に直す。無い・不正なら null。 */
function parseServerDate(res: Response): number | null {
  const raw = res.headers.get("Date");
  if (!raw) {
    return null;
  }
  const ms = Date.parse(raw);
  return Number.isFinite(ms) ? ms : null;
}

/**
 * api() と同じ認証・401 refresh を行い、JSON に加えて応答の Date ヘッダを返す。
 * 204 のとき data は undefined。失敗は ApiError / SignedOutError (api() と同じ)。
 */
export async function apiWithMeta<T>(path: string, req: ApiRequest = {}): Promise<ApiMeta<T>> {
  const url = buildUrl(path, req.query);
  const send = (token: string) => {
    const headers: Record<string, string> = { Authorization: `Bearer ${token}` };
    if (req.body !== undefined) {
      headers["Content-Type"] = "application/json";
    }
    return fetch(url, {
      method: req.method ?? "GET",
      headers,
      body: req.body !== undefined ? JSON.stringify(req.body) : undefined,
      signal: req.signal,
    });
  };

  const token = await session.getAccessToken();
  let res = await send(token);
  if (res.status === 401) {
    const err = await toApiError(res.clone());
    if (err.code !== "unauthenticated") {
      throw err;
    }
    res = await send(await session.recoverFromUnauthorized(token));
  }

  if (!res.ok) {
    throw await toApiError(res);
  }
  const serverDateMs = parseServerDate(res);
  if (res.status === 204) {
    return { data: undefined as T, serverDateMs };
  }
  return { data: (await res.json()) as T, serverDateMs };
}

/**
 * 成功すれば JSON (204 なら undefined) を返す。
 * 失敗は ApiError、未ログイン / refresh 失敗は SignedOutError (このとき useAuth は signedOut になる)。
 */
export async function api<T>(path: string, req: ApiRequest = {}): Promise<T> {
  const meta = await apiWithMeta<T>(path, req);
  return meta.data;
}
