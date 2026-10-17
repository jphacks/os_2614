"use client";

import { useCallback } from "react";
import { ApiError } from "@/auth/errors";
import type { FeedItem as FeedItemDTO } from "@/data/dto";
import { createFeedPost } from "@/data/feed";
import { usePagedResource, type ResourceState } from "@/data/hooks";
import { toFeedPost } from "@/data/mappers";
import type { FeedPost } from "@/domain/types";
import { HomeFeedScreen, type PublicPostResult } from "@/components/screens";

export default function HomePage() {
  const { state, items, hasMore, loadingMore, loadMore, reload } =
    usePagedResource<FeedItemDTO>("/feed?source=home", []);

  const createPublicPost = useCallback(
    async (text: string): Promise<PublicPostResult> => {
      try {
        await createFeedPost({ text });
        reload();
        return { status: "ok" };
      } catch (e) {
        if (e instanceof ApiError) {
          if (e.status === 404 || e.status === 405 || e.status === 501) {
            return { status: "unavailable" };
          }
          return { status: "error", message: e.message };
        }
        return {
          status: "error",
          message: e instanceof Error ? e.message : "投稿に失敗しました",
        };
      }
    },
    [reload],
  );

  const feedState: ResourceState<FeedPost[]> =
    state.status === "ok" ? { status: "ok", data: items.map(toFeedPost) } : state;

  return (
    <HomeFeedScreen
      state={feedState}
      hasMore={hasMore}
      loadingMore={loadingMore}
      onLoadMore={loadMore}
      onRetry={reload}
      onCreatePublicPost={createPublicPost}
    />
  );
}
