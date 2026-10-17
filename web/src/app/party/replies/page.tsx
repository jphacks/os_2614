"use client";

import { Suspense, useCallback, useMemo } from "react";
import { useSearchParams } from "next/navigation";
import { getJson, postJson } from "@/data/api";
import type { CreatedPost, InstancePost, Me, Party as PartyDTO } from "@/data/dto";
import { usePagedResource, useResource, type ResourceState } from "@/data/hooks";
import { toParty, toPost, toProfile, toUser } from "@/data/mappers";
import type { Group, Post, User } from "@/domain/types";
import { GroupReplyScreen } from "@/components/screens/GroupReplyScreen";
import {
  useRealtimeEvent,
  useRealtimeResync,
  useRealtimeSubscription,
} from "@/realtime/provider";

function PartyReplies() {
  const params = useSearchParams();
  const id = params.get("id") ?? "";
  const postId = params.get("post");
  const { state: partyState, reload: reloadParty } = useResource<PartyDTO | null>(
    (signal) => getJson<PartyDTO | null>("/party/current", { signal }),
    [id],
  );
  const { state: postsState, items, reload: reloadPosts } =
    usePagedResource<InstancePost>(id ? `/instances/${id}/posts` : "", [id]);
  const { state: meState } = useResource<Me>(
    (signal) => getJson<Me>("/me", { signal }),
    [],
  );

  const visible = useMemo(
    () => items.filter((item) => !item.hidden && item.moderation?.display !== "hide"),
    [items],
  );
  const posts = useMemo(() => visible.map(toPost), [visible]);
  const opened = posts.find((post) => post.id === postId) ?? posts[0];
  const root = visible.find((item) => item.uri === opened?.id)?.reply?.root ?? opened?.id;
  const replies = useMemo(
    () => visible
      .filter((item) => item.uri !== opened?.id && (item.uri === root || item.reply?.root === root))
      .map((item) => ({
        id: item.uri,
        author: item.author ? toUser(item.author) : toUser(null),
        body: item.text ?? "",
        createdAt: item.createdAt,
        likeCount: 0,
        likedByMe: false,
      })),
    [visible, opened, root],
  );

  useRealtimeSubscription(id || null);
  useRealtimeResync(() => {
    reloadParty();
    reloadPosts();
  });
  useRealtimeEvent("post.created", (post) => {
    if (post.spaceId === id) reloadPosts();
  });

  const submitReply = useCallback(async (post: Post, text: string) => {
    await postJson<{ items: CreatedPost[] }>("/posts", {
      spaceIds: [id],
      text,
      format: "plain",
      reply: { root: visible.find((item) => item.uri === post.id)?.reply?.root ?? post.id, parent: post.id },
    });
    reloadPosts();
  }, [id, reloadPosts, visible]);

  const party = partyState.status === "ok" && partyState.data ? toParty(partyState.data) : null;
  const group: Group | null = party
    ? {
        id: party.id,
        name: party.name,
        description: "",
        visibility: "open",
        memberCount: party.members.length,
        areaLabel: "",
        insideArea: false,
        role: "member",
      }
    : null;
  const groupResource: ResourceState<Group> = group
    ? { status: "ok", data: group }
    : partyState.status === "loading"
      ? { status: "loading" }
      : { status: "empty" };
  const postResource: ResourceState<Post[]> = postsState.status === "ok"
    ? { status: "ok", data: posts }
    : postsState;
  const me: User = meState.status === "ok" ? toProfile(meState.data) : toUser(null);

  return (
    <GroupReplyScreen
      state={groupResource}
      posts={posts}
      postsState={postResource}
      replies={replies}
      me={me}
      closePath={`/party/?id=${encodeURIComponent(id)}`}
      groupRetry={reloadParty}
      postsRetry={reloadPosts}
      onReply={submitReply}
    />
  );
}

export default function PartyRepliesPage() {
  return <Suspense fallback={null}><PartyReplies /></Suspense>;
}
