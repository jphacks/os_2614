"use client";

// /group/replies は static export なので SSR ではデータを取らない。`?id=` の group を
// Space とみなし、ヘッダーは GET /groups/{id}、一覧は GET /instances/{id}/posts (10 §4.2)
// からクライアントでだけ取得する。
//
// 返信は POST /posts の reply:{root,parent} (10 §3, 10 §4.1) で同じ Space に投稿する。
// 取得側も PostItem の reply を使い、開いている投稿と同じ root を持つものだけを
// スレッドとして並べる (返信が返らない場合は空のまま = 推測しない)。
// 送信後は一覧を取り直し、WebSocket の post.created でも他端末の返信を拾う。

import { Suspense, useCallback, useMemo } from "react";
import { useSearchParams } from "next/navigation";
import { getJson, postJson } from "@/data/api";
import type {
  CreatedPost,
  Group as GroupDTO,
  InstancePost,
  Me,
} from "@/data/dto";
import {
  usePagedResource,
  useResource,
  type ResourceState,
} from "@/data/hooks";
import { toGroup, toPost, toProfile, toReply, toUser } from "@/data/mappers";
import type { Group, Post, User } from "@/domain/types";
import { GroupReplyScreen } from "@/components/screens/GroupReplyScreen";
import {
  useRealtimeEvent,
  useRealtimeResync,
  useRealtimeSubscription,
} from "@/realtime/provider";

/** 論理削除 (hidden) と moderation の hide は出さない (10 §4.2:80)。 */
function isVisible(item: InstancePost): boolean {
  return !item.hidden && item.moderation?.display !== "hide";
}

function GroupReplies() {
  const params = useSearchParams();
  // group は Space。spaceId には group の id をそのまま使う (09 §1)。
  const id = params.get("id") ?? "";
  const postId = params.get("post");

  const { state: groupState, reload: reloadGroup } = useResource<GroupDTO>(
    (signal) => getJson<GroupDTO>(`/groups/${id}`, { signal }),
    [id],
    { cacheKey: "/groups/:id" },
  );
  const {
    state: postsState,
    items,
    reload: reloadPosts,
  } = usePagedResource<InstancePost>(id ? `/instances/${id}/posts` : "", [id]);
  // composer のアバター表示に使う。
  const { state: meState } = useResource<Me>(
    (signal) => getJson<Me>("/me", { signal }),
    [],
    { cacheKey: "/me" },
  );

  const visible = useMemo(() => items.filter(isVisible), [items]);
  const posts = useMemo(() => visible.map(toPost), [visible]);

  // 開いている投稿。?post= が一覧に無ければ先頭を開く (GroupReplyScreen と同じ規則)。
  const opened = useMemo(
    () => visible.find((item) => item.uri === postId) ?? visible[0],
    [visible, postId],
  );
  // スレッドの root。開いている投稿が返信ならその root、そうでなければ投稿自身。
  const threadRoot = opened?.reply?.root ?? opened?.uri;

  const replies = useMemo(() => {
    const rows = visible
      .filter(
        (item) =>
          item.uri !== opened?.uri &&
          (item.uri === threadRoot || item.reply?.root === threadRoot),
      )
      .map(toReply);
    // スレッドは古い順に読む。createdAt は backend の RFC3339 をそのまま比較する。
    return rows.sort((a, b) => a.createdAt.localeCompare(b.createdAt));
  }, [visible, opened, threadRoot]);

  const me: User = meState.status === "ok" ? toProfile(meState.data) : toUser(null);

  const submitReply = useCallback(
    async (post: Post, text: string): Promise<void> => {
      const target = visible.find((item) => item.uri === post.id);
      if (!id || !target) {
        throw new Error("返信先の投稿が見つかりません。");
      }
      await postJson<{ items: CreatedPost[] }>("/posts", {
        spaceIds: [id],
        text,
        format: "plain",
        reply: { root: target.reply?.root ?? target.uri, parent: target.uri },
      });
      reloadPosts();
    },
    [id, visible, reloadPosts],
  );

  // 表示中の Space を購読し、返信を含む投稿の追加・削除を取りこぼさない
  // (10 §7.2/7.3)。グループ設定の変更も本体へ反映する。
  useRealtimeSubscription(id || null);
  useRealtimeResync(reloadPosts);
  useRealtimeEvent("post.created", (post) => {
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
      reloadGroup();
    }
  });

  const groupResource: ResourceState<Group> =
    groupState.status === "ok"
      ? { status: "ok", data: toGroup(groupState.data) }
      : groupState;

  const postsResource: ResourceState<Post[]> =
    postsState.status === "ok" ? { status: "ok", data: posts } : postsState;

  return (
    <GroupReplyScreen
      state={groupResource}
      posts={posts}
      postsState={postsResource}
      replies={replies}
      me={me}
      groupRetry={reloadGroup}
      postsRetry={reloadPosts}
      onReply={submitReply}
    />
  );
}

export default function GroupRepliesPage() {
  // useSearchParams は Suspense 境界の内側でだけ使える (static export の prerender 用)。
  return (
    <Suspense fallback={null}>
      <GroupReplies />
    </Suspense>
  );
}
