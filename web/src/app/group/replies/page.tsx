"use client";

import { Suspense, useCallback, useMemo } from "react";
import { useSearchParams } from "next/navigation";
import { getJson } from "@/data/api";
import type { Group as GroupDTO, InstancePost, Instance, Me } from "@/data/dto";
import { usePagedResource, useResource, type ResourceState } from "@/data/hooks";
import { useReplyThreadController } from "@/data/thread";
import { toGroup, toPost, toProfile, toUser } from "@/data/mappers";
import type { Group, Post } from "@/domain/types";
import { GroupReplyScreen } from "@/components/screens/GroupReplyScreen";
import { useRealtimeEvent, useRealtimeResync, useRealtimeSubscription } from "@/realtime/provider";

function GroupReplies() {
  const params = useSearchParams();
  const id = params.get("id") ?? "";
  const postId = params.get("post");
  const groupState = useResource<GroupDTO>((signal) => getJson<GroupDTO>(`/groups/${id}`, { signal }), [id]);
  const posts = usePagedResource<InstancePost>(id ? `/instances/${id}/posts?roots=1` : "", [id]);
  const { state: meState } = useResource<Me>((signal) => getJson<Me>("/me", { signal }), []);
  const thread = useReplyThreadController(id, postId);
  const { reload: reloadPostsPage } = posts;
  const { resetReplyDelta } = thread;
  const instances = useResource<{ items: Array<{ spaceId: string; canWrite: boolean }> }>(
    (signal) => getJson<{ items: Instance[] }>("/instances", { signal }),
    [],
  );
  const canReply = instances.state.status === "ok"
    && instances.state.data.items.some((item) => item.spaceId === id && item.canWrite);
  const reloadPosts = useCallback(() => {
    resetReplyDelta();
    reloadPostsPage();
  }, [reloadPostsPage, resetReplyDelta]);
  const backgroundPosts = useMemo(() => posts.items.map(toPost).map((post) => {
    const delta = thread.replyDelta[post.id] ?? 0;
    return delta === 0 ? post : { ...post, replyCount: post.replyCount + delta };
  }), [posts.items, thread.replyDelta]);

  // 表示中の Space を購読し、返信を含む投稿の追加・削除を取りこぼさない
  // (10 §7.2/7.3)。グループ設定の変更も本体へ反映する。
  useRealtimeSubscription(id || null);
  useRealtimeResync(() => {
    reloadPosts();
    thread.reload();
  });
  useRealtimeEvent("post.created", (post) => {
    if (thread.handlePostCreated(post)) {
      return;
    }
    if (post.spaceId === id) {
      reloadPosts();
    }
  });
  useRealtimeEvent("post.deleted", (_data, frame) => {
    if (frame.space === id) {
      reloadPosts();
    }
  });
  useRealtimeEvent("group.updated", (_data, frame) => {
    if (frame.space === id) {
      groupState.reload();
    }
  });

  const groupResource: ResourceState<Group> = groupState.state.status === "ok"
    ? { status: "ok", data: toGroup(groupState.state.data) }
    : groupState.state;
  const postResource: ResourceState<Post[]> = posts.state.status === "ok"
    ? { status: "ok", data: backgroundPosts }
    : posts.state;
  const me = meState.status === "ok" ? toProfile(meState.data) : toUser(null);
  const threadNotFound = thread.root.status === "unavailable";
  const threadError = thread.root.status === "error"
    ? "返信を読み込めませんでした"
    : undefined;

  return (
    <GroupReplyScreen
      state={groupResource}
      posts={backgroundPosts}
      postsState={postResource}
      replies={thread.replies}
      post={thread.root.status === "ok" ? thread.root.data : null}
      replyTarget={thread.replyTarget}
      me={me}
      closePath={`/group/?id=${encodeURIComponent(id)}`}
      groupRetry={groupState.reload}
      postsRetry={reloadPosts}
      onReply={thread.submitReply}
      hasMore={thread.hasMore}
      loadingMore={thread.loadingMore}
      onLoadMore={thread.loadMore}
      threadLoading={thread.root.status === "loading"}
      threadNotFound={threadNotFound}
      threadError={threadError}
      threadRetry={thread.reload}
      canReply={canReply}
    />
  );
}

export default function GroupRepliesPage() {
  return <Suspense fallback={null}><GroupReplies /></Suspense>;
}
