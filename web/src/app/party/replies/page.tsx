"use client";

import { Suspense, useCallback, useMemo } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { getJson } from "@/data/api";
import type { Instance, InstancePost, Me, Party as PartyDTO } from "@/data/dto";
import { usePagedResource, useResource, type ResourceState } from "@/data/hooks";
import { useReplyThreadController } from "@/data/thread";
import { toParty, toPost, toProfile, toUser } from "@/data/mappers";
import type { Media, Party, Post } from "@/domain/types";
import { PartyScreen } from "@/components/screens/PartyScreen";
import { ReplySheet } from "@/components/feature/post";
import { useRealtimeEvent, useRealtimeResync, useRealtimeSubscription } from "@/realtime/provider";

function PartyReplies() {
  const params = useSearchParams();
  const router = useRouter();
  const id = params.get("id") ?? "";
  const postId = params.get("post");
  const partyState = useResource<PartyDTO | null>((signal) => getJson<PartyDTO | null>("/party/current", { signal }), [id]);
  const posts = usePagedResource<InstancePost>(id ? `/instances/${id}/posts?roots=1` : "", [id]);
  const instances = useResource<{ items: Array<{ spaceId: string; canWrite: boolean }> }>(
    (signal) => getJson<{ items: Instance[] }>("/instances", { signal }),
    [],
  );
  const { state: meState } = useResource<Me>((signal) => getJson<Me>("/me", { signal }), []);
  const thread = useReplyThreadController(id, postId);
  const { reload: reloadPostsPage } = posts;
  const { resetReplyDelta } = thread;
  const root = thread.root.status === "ok" ? thread.root.data : null;
  const party: Party | null = partyState.state.status === "ok" && partyState.state.data
    ? toParty(partyState.state.data)
    : null;
  const backgroundPosts = useMemo(() => posts.items
    .filter((item) => !item.hidden && item.moderation?.display !== "hide")
    .map(toPost)
    .map((post) => {
      const delta = thread.replyDelta[post.id] ?? 0;
      return delta === 0 ? post : { ...post, replyCount: post.replyCount + delta };
    }), [posts.items, thread.replyDelta]);
  const photos: Media[] = backgroundPosts.flatMap((post) => post.attachments);
  const canReply = instances.state.status === "ok"
    && instances.state.data.items.some((item) => item.spaceId === id && item.canWrite);
  const reloadPosts = useCallback(() => {
    resetReplyDelta();
    reloadPostsPage();
  }, [reloadPostsPage, resetReplyDelta]);
  const me = meState.status === "ok" ? toProfile(meState.data) : toUser(null);

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
    if (frame.space === id) reloadPosts();
  });

  const threadNotFound = thread.root.status === "unavailable";
  const threadError = thread.root.status === "error"
    ? "返信を読み込めませんでした"
    : undefined;
  const overlay = (
    <ReplySheet
      post={root}
      replies={thread.replies}
      me={me}
      onClose={() => router.push(`/party/?id=${encodeURIComponent(id)}`)}
      onSubmit={root && canReply && thread.replyTarget
        ? (text) => thread.submitReply(thread.replyTarget!, text)
        : undefined}
      hasMore={thread.hasMore}
      loadingMore={thread.loadingMore}
      onLoadMore={thread.loadMore}
      loading={thread.root.status === "loading"}
      notFound={threadNotFound}
      threadError={threadError}
      onRetry={thread.reload}
      canReply={canReply}
    />
  );
  const partyResource: ResourceState<Party> = partyState.state.status === "ok"
    ? party ? { status: "ok", data: party } : { status: "empty" }
    : partyState.state;
  const postResource: ResourceState<Post[]> = posts.state.status === "ok"
    ? { status: "ok", data: backgroundPosts }
    : posts.state;
  return <PartyScreen state={partyResource} photos={photos} posts={backgroundPosts} postsState={postResource} overlay={overlay} />;
}

export default function PartyRepliesPage() {
  return <Suspense fallback={null}><PartyReplies /></Suspense>;
}
