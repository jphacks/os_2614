// データ層の入口。認証・401 refresh・token 管理は auth/api.ts の api() に任せ、
// ここは「型付け」「{items,cursor} ページング (specification/backend/02 §4)」
// 「UI が分岐しやすい失敗分類」だけを足す薄い層にする。
//
// - URL は api() が API_BASE から組む。ここで URL 文字列を組み立てない。
// - ApiError / SignedOutError は api()/session が投げたものをそのまま扱う。

import { api, apiWithMeta, type ApiMeta, type ApiRequest } from "@/auth/api";
import { ApiError } from "@/auth/errors";
import type { Page } from "./dto";

export type { Page };

/** 送信 body を持たない GET/DELETE と共通の、クエリ・中断のオプション。 */
export interface RequestOptions {
  query?: ApiRequest["query"];
  signal?: AbortSignal;
}

/** GET。 */
export function getJson<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  return api<T>(path, { method: "GET", query: opts.query, signal: opts.signal });
}

/**
 * GET。JSON に加えて応答の Date ヘッダも返す。端末時計のずれを測る呼び出し
 * (beacon token) だけが使う。通常の取得は getJson を使う。
 */
export function getJsonWithMeta<T>(path: string, opts: RequestOptions = {}): Promise<ApiMeta<T>> {
  return apiWithMeta<T>(path, { method: "GET", query: opts.query, signal: opts.signal });
}

/** POST。body は JSON 化して送られる (api() 側)。 */
export function postJson<T>(path: string, body: unknown, opts: RequestOptions = {}): Promise<T> {
  return api<T>(path, { method: "POST", body, query: opts.query, signal: opts.signal });
}

/** POST multipart/form-data (POST /media、10 §4.4)。 */
export function postForm<T>(path: string, form: FormData, opts: RequestOptions = {}): Promise<T> {
  return api<T>(path, { method: "POST", form, query: opts.query, signal: opts.signal });
}

/** PUT multipart/form-data (PUT /me/avatar、PUT /me/banner、13 §1.3)。 */
export function putForm<T>(path: string, form: FormData, opts: RequestOptions = {}): Promise<T> {
  return api<T>(path, { method: "PUT", form, query: opts.query, signal: opts.signal });
}

/** PATCH。 */
export function patchJson<T>(path: string, body: unknown, opts: RequestOptions = {}): Promise<T> {
  return api<T>(path, { method: "PATCH", body, query: opts.query, signal: opts.signal });
}

/** PUT。 */
export function putJson<T>(path: string, body: unknown, opts: RequestOptions = {}): Promise<T> {
  return api<T>(path, { method: "PUT", body, query: opts.query, signal: opts.signal });
}

/** DELETE。204 なら T は undefined になる。 */
export function deleteJson<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  return api<T>(path, { method: "DELETE", query: opts.query, signal: opts.signal });
}

/** fetchPage / fetchAllPages の入力。limit・cursor は未指定なら URL に載せない。 */
export interface PageOptions {
  limit?: number;
  cursor?: string;
  signal?: AbortSignal;
}

/**
 * `?limit=&cursor=` で 1 ページ取得する。backend が次ページを返さないときは
 * cursor が undefined になる (このキーは常に生える)。
 */
export async function fetchPage<T>(path: string, opts: PageOptions = {}): Promise<Page<T>> {
  const page = await getJson<Page<T>>(path, {
    query: { limit: opts.limit, cursor: opts.cursor },
    signal: opts.signal,
  });
  return { items: page.items, cursor: page.cursor };
}

/**
 * cursor を辿って 2 ページ以上を 1 本の配列にまとめる。
 * 「もっと見る」で全件を読み切るときに使う (UI が最初の fetchPage を表示済みでも重複しない)。
 */
export async function fetchAllPages<T>(
  path: string,
  opts: { limit?: number; signal?: AbortSignal } = {},
): Promise<T[]> {
  const items: T[] = [];
  let cursor: string | undefined;
  do {
    const page = await fetchPage<T>(path, { limit: opts.limit, cursor, signal: opts.signal });
    items.push(...page.items);
    cursor = page.cursor;
  } while (cursor !== undefined);
  return items;
}

/** 404/405/501 は「リソースが無い / 未対応」。UI は空表示にでき、エラー扱いにしない。 */
const UNAVAILABLE_STATUSES = new Set([404, 405, 501]);

/** api() の失敗を UI が分岐できる 3 状態に畳んだ結果。 */
export type ResourceResult<T> =
  | { status: "ok"; data: T }
  | { status: "unavailable"; error: ApiError }
  | { status: "error"; error: ApiError };

/**
 * api() 呼び出しを包んで ok / unavailable / error に分類する。
 * - unavailable: HTTP 404/405/501
 * - error: それ以外の失敗 (5xx、400、ネットワーク断など)
 */
export async function toResult<T>(fn: () => Promise<T>): Promise<ResourceResult<T>> {
  try {
    return { status: "ok", data: await fn() };
  } catch (e) {
    if (e instanceof ApiError) {
      if (UNAVAILABLE_STATUSES.has(e.status)) {
        return { status: "unavailable", error: e };
      }
      return { status: "error", error: e };
    }
    // ApiError でない失敗 (fetch の TypeError など) も error に寄せる。
    const message = e instanceof Error ? e.message : String(e);
    return { status: "error", error: new ApiError(0, "network", message) };
  }
}
