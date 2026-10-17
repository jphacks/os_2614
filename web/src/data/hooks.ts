"use client";

// 画面が「読み込み中 / 成功 / 空 / エラー / 未対応」を出し分けるための、
// クライアント専用のデータ取得フック。
//
// 守っていること:
// - 取得そのもの (URL 組み立て・型付け・401 refresh) は ./api に任せる。ここは
//   状態遷移と AbortController の後始末だけを持つ薄い層にする。
// - static export なので SSR では走らない。取得は useEffect の中でだけ行う。
// - 未ログイン (auth state が signedIn でない) では取得しない。サインイン後にだけ走らせる。
// - unmount / deps 変更で AbortController.abort() を呼び、遅延レスポンスを捨てる。
//
// 空 (status: "empty") は ok の特別形として扱う。ok と empty を分けるのは、
// 画面が「データが 1 件も無い」と「データがある」を別々に出し分けられるようにするため。
//
// effect 本体で同期 setState すると react-hooks/set-state-in-effect に触れるため、
// 「どのリクエストの結果か」を key に持つ state を置き、key が一致しないときは
// 描画側で loading に倒す (stale なデータを出さない)。
//
// ただし画面遷移のたびに loading を挟まないよう、取得に成功した結果はモジュール
// スコープのキャッシュに残す (stale-while-revalidate)。同じリソースを再び開いたら
// キャッシュを即座に出しつつ裏で取り直し、レスポンスが返ったら差し替える。
// キャッシュのキーには DID を含め、別アカウントの結果を出さない。

import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "@/auth";
import { ApiError } from "@/auth/errors";
import type { Settings } from "@/domain/types";
import { fetchPage, patchJson, putJson, toResult, type ResourceResult } from "./api";
import { fromSettings } from "./mappers";

/** UI が出し分ける 5 状態。empty は ok から派生する (ok かつ空の特別形)。 */
export type ResourceState<T> =
  | { status: "loading" }
  | { status: "ok"; data: T }
  | { status: "empty" }
  | { status: "error"; error: ApiError }
  | { status: "unavailable"; error: ApiError };

/**
 * api() の分類結果 (ok / unavailable / error) を UI の 5 状態に畳む純関数。
 * isEmpty が true を返したときだけ ok を empty に落とす。未指定なら ok のまま。
 * unavailable (404/405/501) は「まだ無い / 未対応」で、エラーとは区別する。
 */
export function deriveState<T>(
  result: ResourceResult<T>,
  isEmpty?: (data: T) => boolean,
): ResourceState<T> {
  switch (result.status) {
    case "ok":
      return isEmpty?.(result.data) ? { status: "empty" } : { status: "ok", data: result.data };
    case "unavailable":
      return { status: "unavailable", error: result.error };
    case "error":
      return { status: "error", error: result.error };
  }
}

/**
 * 取得に成功した結果 (ok / empty) のキャッシュ。キーは `${did}\u0000${リソース}\u0000${deps}`。
 * error / unavailable は残さず、該当キーを消す。
 */
const resourceCache = new Map<string, unknown>();

/**
 * deps を effect の依存に使える 1 本のキーに畳む。
 * オブジェクトは JSON 化する (画面が渡す deps は ID や文字列などの原始値が主)。
 */
function depsKey(deps: readonly unknown[]): string {
  return deps
    .map((dep) => {
      if (dep === null || typeof dep !== "object") {
        return String(dep);
      }
      try {
        return JSON.stringify(dep);
      } catch {
        return String(dep);
      }
    })
    .join("\u0000");
}

export interface ResourceOptions<T> {
  /** ok の data を「空」とみなす条件。未指定なら常に ok のまま。 */
  isEmpty?: (data: T) => boolean;
  /**
   * 結果をキャッシュするときのリソース名 (例: "/me")。deps と組み合わせてキーにする。
   * 指定すると再訪時にキャッシュを即座に出し、裏で取り直す。未指定ならキャッシュしない。
   */
  cacheKey?: string;
}

export interface Resource<T> {
  state: ResourceState<T>;
  /** 同じ deps でもう一度取得する。 */
  reload: () => void;
}

/**
 * 単一リソースを取得する。fetcher は signal を受けて 1 つの値を返す
 * (例: `(signal) => getJson<User>("/me", { signal })`)。deps が変わると再取得する。
 */
export function useResource<T>(
  fetcher: (signal: AbortSignal) => Promise<T>,
  deps: readonly unknown[],
  opts: ResourceOptions<T> = {},
): Resource<T> {
  const { state: authState } = useAuth();
  const signedIn = authState.status === "signedIn";
  const [nonce, setNonce] = useState(0);
  const requestKey = signedIn ? `${depsKey(deps)}\u0000${nonce}` : "";
  const cacheKey =
    authState.status === "signedIn" && opts.cacheKey !== undefined
      ? `${authState.did}\u0000${opts.cacheKey}\u0000${depsKey(deps)}`
      : "";
  const [entry, setEntry] = useState<{ key: string; state: ResourceState<T> }>({
    key: "",
    state: { status: "loading" },
  });

  // 最新の fetcher / isEmpty を effect から読む。依存配列を汚さないため ref に持つ。
  const latest = useRef({ fetcher, isEmpty: opts.isEmpty });
  useEffect(() => {
    latest.current = { fetcher, isEmpty: opts.isEmpty };
  });

  useEffect(() => {
    if (requestKey === "") {
      return;
    }
    const controller = new AbortController();
    let active = true;
    void toResult(() => latest.current.fetcher(controller.signal)).then((result) => {
      if (!active) {
        return;
      }
      const next = deriveState(result, latest.current.isEmpty);
      if (cacheKey !== "") {
        if (next.status === "ok" || next.status === "empty") {
          resourceCache.set(cacheKey, next);
        } else {
          resourceCache.delete(cacheKey);
        }
      }
      setEntry({ key: requestKey, state: next });
    });
    return () => {
      active = false;
      controller.abort();
    };
  }, [requestKey, cacheKey]);

  const cached =
    cacheKey !== "" ? (resourceCache.get(cacheKey) as ResourceState<T> | undefined) : undefined;
  const state: ResourceState<T> =
    requestKey !== "" && entry.key === requestKey
      ? entry.state
      : (cached ?? { status: "loading" });

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  return { state, reload };
}

export interface PagedResourceOptions<T> {
  /** 1 ページの件数。未指定なら backend の既定。 */
  limit?: number;
  /** 蓄積後の items を「空」とみなす条件。既定は「1 件も無い」。 */
  isEmpty?: (items: T[]) => boolean;
}

export interface PagedResource<T> extends Resource<T[]> {
  /** 取得済みの全 item (ページを跨いで蓄積)。 */
  items: T[];
  /** backend がまだ次の cursor を返しているか。 */
  hasMore: boolean;
  /** loadMore() の取得中か。 */
  loadingMore: boolean;
  /** 次の 1 ページを末尾に足す。続きが無い / 取得中 / 未ログインなら何もしない。 */
  loadMore: () => void;
}

interface PagedEntry<T> {
  key: string;
  state: ResourceState<T[]>;
  items: T[];
  hasMore: boolean;
  loadingMore: boolean;
}

/** usePagedResource がキャッシュする 1 ページ目の結果。loadMore で足した分は含めない。 */
interface PagedCacheEntry<T> {
  state: ResourceState<T[]>;
  items: T[];
  hasMore: boolean;
}

function emptyPaged<T>(key: string): PagedEntry<T> {
  return { key, state: { status: "loading" }, items: [], hasMore: false, loadingMore: false };
}

/**
 * `{items, cursor}` の一覧を取得し、「もっと見る」でページを蓄積する。
 * 初回は fetchPage を 1 回だけ呼び、以降 loadMore() で cursor を辿って末尾に足す。
 */
export function usePagedResource<T>(
  path: string,
  deps: readonly unknown[],
  opts: PagedResourceOptions<T> = {},
): PagedResource<T> {
  const { state: authState } = useAuth();
  const signedIn = authState.status === "signedIn";
  const [nonce, setNonce] = useState(0);
  // path / deps / reload を 1 本の key に畳む。key が変わると描画側で loading に倒す。
  const requestKey = signedIn ? `${path}\u0000${depsKey(deps)}\u0000${nonce}` : "";
  const cacheKey =
    authState.status === "signedIn" && path !== ""
      ? `${authState.did}\u0000${path}\u0000${depsKey(deps)}`
      : "";

  const [page, setPage] = useState<PagedEntry<T>>(() => emptyPaged<T>(""));
  const cursorRef = useRef<string | undefined>(undefined);
  const inFlightRef = useRef(false);
  // setItems の非同期性を跨いで蓄積配列を組み立てるための写し。
  const itemsRef = useRef<T[]>([]);
  // loadMore の取得を deps 変更 / unmount で中断するための保持。
  const moreRef = useRef<AbortController | null>(null);

  // 最新の limit / isEmpty を effect から読む。
  const latest = useRef({ limit: opts.limit, isEmpty: opts.isEmpty });
  useEffect(() => {
    latest.current = { limit: opts.limit, isEmpty: opts.isEmpty };
  });

  useEffect(() => {
    if (requestKey === "") {
      return;
    }
    cursorRef.current = undefined;
    inFlightRef.current = false;
    moreRef.current?.abort();
    const controller = new AbortController();
    let active = true;
    void toResult(() =>
      fetchPage<T>(path, { limit: latest.current.limit, signal: controller.signal }),
    ).then((result) => {
      if (!active) {
        return;
      }
      if (result.status === "ok") {
        cursorRef.current = result.data.cursor;
        itemsRef.current = result.data.items;
        const first: PagedCacheEntry<T> = {
          state: deriveState({ status: "ok", data: result.data.items }, latest.current.isEmpty),
          items: result.data.items,
          hasMore: result.data.cursor !== undefined,
        };
        if (cacheKey !== "") {
          resourceCache.set(cacheKey, first);
        }
        setPage({ key: requestKey, ...first, loadingMore: false });
      } else {
        if (cacheKey !== "") {
          resourceCache.delete(cacheKey);
        }
        itemsRef.current = [];
        setPage({
          key: requestKey,
          state:
            result.status === "unavailable"
              ? { status: "unavailable", error: result.error }
              : { status: "error", error: result.error },
          items: [],
          hasMore: false,
          loadingMore: false,
        });
      }
    });
    return () => {
      active = false;
      controller.abort();
      moreRef.current?.abort();
    };
  }, [requestKey, path, cacheKey]);

  const loadMore = useCallback(() => {
    const cursor = cursorRef.current;
    if (requestKey === "" || cursor === undefined || inFlightRef.current) {
      return;
    }
    inFlightRef.current = true;
    setPage((prev) => (prev.key === requestKey ? { ...prev, loadingMore: true } : prev));
    const controller = new AbortController();
    moreRef.current = controller;
    void toResult(() =>
      fetchPage<T>(path, { limit: latest.current.limit, cursor, signal: controller.signal }),
    ).then((result) => {
      if (controller.signal.aborted) {
        return;
      }
      inFlightRef.current = false;
      if (result.status === "ok") {
        const next = [...itemsRef.current, ...result.data.items];
        cursorRef.current = result.data.cursor;
        itemsRef.current = next;
        setPage((prev) =>
          prev.key === requestKey
            ? {
                key: requestKey,
                state: deriveState({ status: "ok", data: next }, latest.current.isEmpty),
                items: next,
                hasMore: result.data.cursor !== undefined,
                loadingMore: false,
              }
            : prev,
        );
      } else {
        setPage((prev) =>
          prev.key === requestKey
            ? {
                ...prev,
                state:
                  result.status === "unavailable"
                    ? { status: "unavailable", error: result.error }
                    : { status: "error", error: result.error },
                loadingMore: false,
              }
            : prev,
        );
      }
    });
  }, [requestKey, path]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);

  // 取り直し中はキャッシュした 1 ページ目を出す。cursor は取り直しが終わるまで持たないので、
  // その間の loadMore は何もしない (古い cursor で続きを足して一覧が混ざるのを避ける)。
  const cached =
    cacheKey !== "" ? (resourceCache.get(cacheKey) as PagedCacheEntry<T> | undefined) : undefined;
  const current =
    requestKey !== "" && page.key === requestKey
      ? page
      : cached
        ? { key: requestKey, ...cached, loadingMore: false }
        : emptyPaged<T>(requestKey);

  return {
    state: current.state,
    items: current.items,
    hasMore: current.hasMore,
    loadingMore: current.loadingMore,
    reload,
    loadMore,
  };
}

/** 設定保存のデバウンス幅 (ms)。連続トグルを 1 リクエストに畳む。 */
export const SETTINGS_SAVE_DEBOUNCE_MS = 400;

function hasPatchKeys(patch: Partial<Settings>): boolean {
  return Object.keys(patch).length > 0;
}

export interface SettingsPersistence {
  /** 楽観更新を反映した設定の取得状態。元が ok のときだけ ok になる。 */
  state: ResourceState<Settings>;
  /** 変更されたキーだけを受ける。表示は即時反映し、デバウンスして保存する。 */
  onChange: (patch: Partial<Settings>) => void;
  saving: boolean;
  /** 直近の保存失敗。成功で消える。 */
  error: ApiError | null;
  /** 失敗した保存を同じ内容で再送する。 */
  retry: () => void;
}

/**
 * 設定の保存を、楽観更新 + デバウンス + 失敗ロールバックで扱う。
 *
 * - `PATCH /me/settings` には変更キーだけを載せる (`fromSettings`)。`locationPaused`
 *   は `PUT /me/location-sharing {paused}` が所有し、PATCH には含めない (todo 35 の
 *   `contentScanConsent` も同様に fromSettings が弾く)。
 * - 失敗したら直前に確定した値へ戻し、`error` を立てる。`retry()` で再送できる。
 * - unmount でデバウンスタイマーと送信中のリクエストを片付ける。
 *
 * setState は effect 本体では呼ばない (react-hooks/set-state-in-effect 回避)。
 * 楽観値は「どの server リビジョンを基にしたか」を key に持ち、key が変わったら
 * server 値に戻す (useResource と同じ keyed-state パターン)。
 */
export function useSettingsPersistence(
  state: ResourceState<Settings>,
): SettingsPersistence {
  const serverSettings = state.status === "ok" ? state.data : null;
  const serverKey = serverSettings ? JSON.stringify(serverSettings) : "";

  const [optimistic, setOptimistic] = useState<{ key: string; value: Settings | null }>({
    key: "",
    value: null,
  });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  const serverRef = useRef<Settings | null>(serverSettings);
  // 表示中の楽観値。flush が送信スナップショットとして読む。
  const valueRef = useRef<Settings | null>(serverSettings);
  // 直近でサーバに受理された値。失敗時のロールバック先。
  const confirmedRef = useRef<Settings | null>(serverSettings);
  // 失敗した変更 (retry 用)。
  const failedRef = useRef<Partial<Settings> | null>(null);
  // 未送信の変更。
  const pendingRef = useRef<Partial<Settings>>({});
  const inflightRef = useRef(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const mountedRef = useRef(true);
  const flushRef = useRef<() => Promise<void>>(async () => {});
  const scheduleRef = useRef<() => void>(() => {});

  // 最新の server 値を毎レンダー取り込む (deps 無しの effect なので lint を汚さない)。
  useEffect(() => {
    serverRef.current = serverSettings;
  });

  // server リビジョンが変わったら (初回 ok / reload)、楽観値と確定値を server に戻す。
  useEffect(() => {
    if (optimistic.key !== serverKey) {
      valueRef.current = serverSettings;
      confirmedRef.current = serverSettings;
      failedRef.current = null;
      pendingRef.current = {};
    }
  });

  const scheduleFlush = useCallback(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
    }
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      void flushRef.current();
    }, SETTINGS_SAVE_DEBOUNCE_MS);
  }, []);

  const flush = useCallback(async () => {
    // 送信中に来た変更は、完了後の finally で拾い直す。
    if (inflightRef.current) {
      return;
    }
    const patch = pendingRef.current;
    if (!hasPatchKeys(patch)) {
      return;
    }
    pendingRef.current = {};
    const sentValue = valueRef.current;
    const controller = new AbortController();
    abortRef.current = controller;
    inflightRef.current = true;
    setSaving(true);
    try {
      const settingsPatch = fromSettings(patch);
      if (hasPatchKeys(settingsPatch)) {
        await patchJson<unknown>("/me/settings", settingsPatch, {
          signal: controller.signal,
        });
      }
      if (patch.locationPaused !== undefined) {
        await putJson<void>(
          "/me/location-sharing",
          { paused: patch.locationPaused },
          { signal: controller.signal },
        );
      }
      if (controller.signal.aborted || !mountedRef.current) {
        return;
      }
      confirmedRef.current = sentValue;
      failedRef.current = null;
      setError(null);
    } catch (e) {
      if (controller.signal.aborted || !mountedRef.current) {
        return;
      }
      failedRef.current = patch;
      const revert = confirmedRef.current ?? serverRef.current;
      valueRef.current = revert;
      setOptimistic({ key: serverKey, value: revert });
      const message = e instanceof Error ? e.message : String(e);
      setError(e instanceof ApiError ? e : new ApiError(0, "network", message));
    } finally {
      abortRef.current = null;
      inflightRef.current = false;
      if (mountedRef.current) {
        if (hasPatchKeys(pendingRef.current)) {
          scheduleRef.current();
        } else {
          setSaving(false);
        }
      }
    }
  }, [serverKey]);

  // flush から最新の flush / scheduleFlush を呼ぶ (相互参照の循環を避ける)。
  useEffect(() => {
    flushRef.current = flush;
    scheduleRef.current = scheduleFlush;
  });

  // unmount でタイマーと送信中のリクエストを止める。StrictMode の疑似 unmount 後に
  // 再実行されるため、effect 本体で mounted を立て直す (ref は remount を跨いで残る)。
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
      abortRef.current?.abort();
    };
  }, []);

  const update = useCallback(
    (patch: Partial<Settings>) => {
      if (!hasPatchKeys(patch)) {
        return;
      }
      const base = valueRef.current ?? serverRef.current;
      if (!base) {
        return;
      }
      const next = { ...base, ...patch };
      valueRef.current = next;
      pendingRef.current = { ...pendingRef.current, ...patch };
      setOptimistic({ key: serverKey, value: next });
      setError(null);
      scheduleFlush();
    },
    [serverKey, scheduleFlush],
  );

  const retry = useCallback(() => {
    const failed = failedRef.current;
    if (!failed) {
      return;
    }
    failedRef.current = null;
    const base = confirmedRef.current ?? serverRef.current;
    if (!base) {
      return;
    }
    const next = { ...base, ...failed };
    valueRef.current = next;
    pendingRef.current = { ...pendingRef.current, ...failed };
    setOptimistic({ key: serverKey, value: next });
    setError(null);
    void flushRef.current();
  }, [serverKey]);

  const current =
    serverSettings === null
      ? null
      : optimistic.key === serverKey && optimistic.value
        ? optimistic.value
        : serverSettings;

  const outState: ResourceState<Settings> =
    serverSettings === null ? state : { status: "ok", data: current ?? serverSettings };

  return { state: outState, onChange: update, saving, error, retry };
}
