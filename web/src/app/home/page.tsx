"use client";

// フィードタブ。GET /feed?source=home|groups (17 §3.1) をクライアントで取り、
// ホーム / グループを切り替える。投稿は右下のボタンから Compose で行う。
//
// ユーザー宛ての feed.updated (10 §7.3, 17 §3.4) は「表示中の source に新着が入った」
// ことだけを知らせるので、同じ source のときだけ GET /feed を取り直す。Jetstream の
// 取り込みなどで連続して届くため、1 秒まとめてから 1 回だけ取り直す。

import { useCallback, useEffect, useRef, useState } from "react";
import type { FeedItem as FeedItemDTO } from "@/data/dto";
import { usePagedResource, type ResourceState } from "@/data/hooks";
import { toFeedPost } from "@/data/mappers";
import type { FeedPost } from "@/domain/types";
import { HomeFeedScreen, type FeedSource } from "@/components/screens";
import { useRealtimeEvent, useRealtimeResync } from "@/realtime/provider";

/** feed.updated が連続で届く間、取り直しをまとめる幅 (ms)。 */
const FEED_RELOAD_DEBOUNCE_MS = 1000;

export default function HomePage() {
  const [source, setSource] = useState<FeedSource>("home");
  const { state, items, hasMore, loadingMore, loadMore, reload } =
    usePagedResource<FeedItemDTO>(`/feed?source=${source}`, []);

  // 連続する feed.updated を 1 回の reload に畳む。アンマウントでタイマーを消す。
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const scheduleReload = useCallback(() => {
    if (timer.current !== null) {
      clearTimeout(timer.current);
    }
    timer.current = setTimeout(() => {
      timer.current = null;
      reload();
    }, FEED_RELOAD_DEBOUNCE_MS);
  }, [reload]);

  useEffect(
    () => () => {
      if (timer.current !== null) {
        clearTimeout(timer.current);
        timer.current = null;
      }
    },
    [],
  );

  useRealtimeEvent("feed.updated", (event) => {
    if (event.source === source) {
      scheduleReload();
    }
  });
  useRealtimeResync(reload);

  const feedState: ResourceState<FeedPost[]> =
    state.status === "ok" ? { status: "ok", data: items.map(toFeedPost) } : state;

  return (
    <HomeFeedScreen
      source={source}
      onSourceChange={setSource}
      state={feedState}
      hasMore={hasMore}
      loadingMore={loadingMore}
      onLoadMore={loadMore}
      onRetry={reload}
    />
  );
}
