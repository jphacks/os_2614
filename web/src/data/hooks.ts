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

import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "@/auth";
import { ApiError } from "@/auth/errors";
import { fetchPage, toResult, type ResourceResult } from "./api";

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
      setEntry({ key: requestKey, state: deriveState(result, latest.current.isEmpty) });
    });
    return () => {
      active = false;
      controller.abort();
    };
  }, [requestKey]);

  const state: ResourceState<T> =
    requestKey !== "" && entry.key === requestKey ? entry.state : { status: "loading" };

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
        setPage({
          key: requestKey,
          state: deriveState({ status: "ok", data: result.data.items }, latest.current.isEmpty),
          items: result.data.items,
          hasMore: result.data.cursor !== undefined,
          loadingMore: false,
        });
      } else {
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
  }, [requestKey, path]);

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

  const current = requestKey !== "" && page.key === requestKey ? page : emptyPaged<T>(requestKey);

  return {
    state: current.state,
    items: current.items,
    hasMore: current.hasMore,
    loadingMore: current.loadingMore,
    reload,
    loadMore,
  };
}
