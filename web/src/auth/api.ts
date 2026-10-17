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

/**
 * 成功すれば JSON (204 なら undefined) を返す。
 * 失敗は ApiError、未ログイン / refresh 失敗は SignedOutError (このとき useAuth は signedOut になる)。
 */
export async function api<T>(path: string, req: ApiRequest = {}): Promise<T> {
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
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}
