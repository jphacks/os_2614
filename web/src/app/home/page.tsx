"use client";

// フィードタブ。GET /feed?source=home|groups (17 §3.1) をクライアントで取り、
// ホーム / グループを切り替える。投稿は右下のボタンから Compose で行う。

import { useState } from "react";
import type { FeedItem as FeedItemDTO } from "@/data/dto";
import { usePagedResource, type ResourceState } from "@/data/hooks";
import { toFeedPost } from "@/data/mappers";
import type { FeedPost } from "@/domain/types";
import { HomeFeedScreen, type FeedSource } from "@/components/screens";

export default function HomePage() {
  const [source, setSource] = useState<FeedSource>("home");
  const { state, items, hasMore, loadingMore, loadMore, reload } =
    usePagedResource<FeedItemDTO>(`/feed?source=${source}`, []);

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
