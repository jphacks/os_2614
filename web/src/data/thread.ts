"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import { getJson, postJson } from "./api";
import { usePagedResource, type ResourceState } from "./hooks";
import type { InstancePost, ThreadResponse } from "./dto";
import { toPost, toReply } from "./mappers";
import type { Post, Reply } from "@/domain/types";

export type ThreadResource = {
  root: ResourceState<Post>;
  replies: Reply[];
  repliesState: ResourceState<Reply[]>;
  hasMore: boolean;
  loadingMore: boolean;
  loadMore: () => void;
  reload: () => void;
  append: (post: InstancePost) => void;
};

/** 10 §4.2.1 のスレッド API を取得し、返信のページングとリアルタイム追加を共通化する。 */
export function useThread(spaceId: string, rootURI: string | null): ThreadResource {
  const path = spaceId ? `/instances/${encodeURIComponent(spaceId)}/thread` : "";
  const rootRef = useRef<InstancePost | null>(null);
  const [rootPost, setRootPost] = useState<InstancePost | null>(null);
  const page = usePagedResource<InstancePost>(path, [spaceId, rootURI], {
    fetchPage: (requestPath, options) => {
      if (options?.cursor === undefined) {
        rootRef.current = null;
        setRootPost(null);
      }
      return getJson<ThreadResponse>(requestPath, {
        query: { root: rootURI ?? "", cursor: options?.cursor, limit: options?.limit },
        signal: options?.signal,
      }).then((response) => {
        if (options?.cursor === undefined) {
          rootRef.current = response.root;
          setRootPost(response.root);
        }
        return { items: response.replies ?? [], cursor: response.cursor };
      });
    },
  });
  const [live, setLive] = useState<InstancePost[]>([]);
  const append = useCallback(
    (post: InstancePost) => {
      if (!post.reply?.root || post.reply.root !== (rootRef.current?.uri ?? rootURI)) {
        return;
      }
      setLive((current) => [...current.filter((item) => item.uri !== post.uri), post]);
    },
    [rootURI],
  );
  const replyDTOs = useMemo(() => {
    const seen = new Set<string>();
    return [...page.items, ...live].filter((item) => !seen.has(item.uri) && seen.add(item.uri));
  }, [page.items, live]);
  const replies = useMemo(
    () =>
      replyDTOs.map((item) => {
        const reply = toReply(item);
        const parent = replyDTOs.find((candidate) => candidate.uri === item.reply?.parent);
        return parent
          ? { ...reply, parentAuthorName: parent.author?.displayName ?? parent.author?.handle }
          : reply;
      }),
    [replyDTOs],
  );
  const rootState: ResourceState<Post> =
    page.state.status === "ok" && rootPost
      ? { status: "ok", data: toPost(rootPost) }
      : page.state.status === "error" || page.state.status === "unavailable"
        ? page.state
        : { status: "loading" };
  return {
    root: rootState,
    replies,
    repliesState: page.state.status === "ok" ? { status: "ok", data: replies } : page.state,
    hasMore: page.hasMore,
    loadingMore: page.loadingMore,
    loadMore: page.loadMore,
    reload: page.reload,
    append,
  };
}

export type ReplyThreadController = ThreadResource & {
  replyTarget: Pick<Post, "id"> | null;
  replyDelta: Record<string, number>;
  resetReplyDelta: () => void;
  handlePostCreated: (post: InstancePost) => boolean;
  submitReply: (parent: Pick<Post, "id">, text: string) => Promise<void>;
};

/** 10 §4.2 / §4.2.1 の返信表示・投稿・リアルタイム反映を共通化する。 */
export function useReplyThreadController(
  spaceId: string,
  rootURI: string | null,
): ReplyThreadController {
  const thread = useThread(spaceId, rootURI);
  const { append, reload } = thread;
  const [replyDelta, setReplyDelta] = useState<Record<string, number>>({});
  const sentReplies = useRef(new Set<string>());
  const root = thread.root.status === "ok" ? thread.root.data : null;
  const replyTarget = thread.replies.find((reply) => reply.id === rootURI) ?? root;

  const handlePostCreated = useCallback(
    (post: InstancePost) => {
      if (post.spaceId !== spaceId || !post.reply?.root) {
        return false;
      }
      append(post);
      const replyRoot = post.reply.root;
      if (!sentReplies.current.delete(post.uri)) {
        setReplyDelta((current) => ({
          ...current,
          [replyRoot]: (current[replyRoot] ?? 0) + 1,
        }));
      }
      return true;
    },
    [append, spaceId],
  );

  const submitReply = useCallback(
    async (parent: Pick<Post, "id">, text: string) => {
      if (!root) {
        throw new Error("この投稿は見つかりません");
      }
      const result = await postJson<{ items: Array<{ uri?: string }> }>("/posts", {
        spaceIds: [spaceId],
        text,
        format: "plain",
        reply: { root: root.id, parent: parent.id },
      });
      if (result.items[0]?.uri) {
        sentReplies.current.add(result.items[0].uri);
      }
      setReplyDelta((current) => ({
        ...current,
        [root.id]: (current[root.id] ?? 0) + 1,
      }));
      reload();
    },
    [reload, root, spaceId],
  );

  const resetReplyDelta = useCallback(() => setReplyDelta({}), []);
  return { ...thread, replyTarget, replyDelta, resetReplyDelta, handlePostCreated, submitReply };
}
