"use client";

// /group は static export なので SSR ではデータを取らない。`?id=` の group を
// Space とみなし (09 §1)、ヘッダーは GET /groups/{id} (09 §4)、タイムラインは
// GET /instances/{id}/posts (10 §4.2) からクライアントでだけ取得する。
// DTO は toGroup / toPost でドメインへ写し、UI には DTO を渡さない。

import { Suspense, useMemo } from "react";
import { useSearchParams } from "next/navigation";
import { getJson } from "@/data/api";
import type { Group as GroupDTO, InstancePost } from "@/data/dto";
import {
  usePagedResource,
  useResource,
  type ResourceState,
} from "@/data/hooks";
import { toGroup, toPost } from "@/data/mappers";
import type { Group, Post } from "@/domain/types";
import { GroupScreen } from "@/components/screens/GroupScreen";
import {
  useRealtimeEvent,
  useRealtimeResync,
  useRealtimeSubscription,
} from "@/realtime/provider";

/**
 * 論理削除 (hidden) の投稿と、moderation の表示指示が hide の投稿は出さない
 * (10 §4.2:80)。表示対象だけをドメイン Post へ写す。
 */
function toTimelinePosts(items: InstancePost[]): Post[] {
  return items
    .filter((item) => !item.hidden && item.moderation?.display !== "hide")
    .map(toPost);
}

function GroupTimeline() {
  const params = useSearchParams();
  // group は Space。spaceId には group の id をそのまま使う (09 §1)。
  const id = params.get("id") ?? "";

  const { state: groupState, reload: reloadGroup } = useResource<GroupDTO>(
    (signal) => getJson<GroupDTO>(`/groups/${id}`, { signal }),
    [id],
  );
  const {
    state: postsState,
    items,
    hasMore,
    loadingMore,
    loadMore,
    reload: reloadPosts,
  } = usePagedResource<InstancePost>(id ? `/instances/${id}/posts` : "", [id]);

  const posts = useMemo(() => toTimelinePosts(items), [items]);

  // 表示中のグループ (Space) を購読し、リアルタイム更新を反映する (10 §7.2)。
  useRealtimeSubscription(id);
  // 再接続後は at-most-once の取りこぼしを補うためタイムラインを取り直す (10 §7.3)。
  useRealtimeResync(reloadPosts);
  useRealtimeEvent("post.created", (post) => {
    if (post.spaceId === id) {
      reloadPosts();
    }
  });

  // ok のときだけ DTO をドメインへ写す。それ以外は取得状態をそのまま渡す。
  const groupResource: ResourceState<Group> =
    groupState.status === "ok"
      ? { status: "ok", data: toGroup(groupState.data) }
      : groupState;

  const postsResource: ResourceState<Post[]> =
    postsState.status === "ok"
      ? { status: "ok", data: posts }
      : postsState;

  return (
    <GroupScreen
      state={groupResource}
      posts={posts}
      postsState={postsResource}
      hasMore={hasMore}
      loadingMore={loadingMore}
      onLoadMore={loadMore}
      groupRetry={reloadGroup}
      postsRetry={reloadPosts}
    />
  );
}

export default function GroupPage() {
  // useSearchParams は Suspense 境界の内側でだけ使える (static export の prerender 用)。
  return (
    <Suspense fallback={null}>
      <GroupTimeline />
    </Suspense>
  );
}
