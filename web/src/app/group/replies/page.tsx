"use client";

// /group/replies は static export なので SSR ではデータを取らない。`?id=` の group を
// Space とみなし、ヘッダーは GET /groups/{id}、一覧は GET /instances/{id}/posts (10 §4.2)
// からクライアントでだけ取得する。返信スレッドは backend 未対応 (P1、独立エンドポイント無し、
// POST /posts に reply フィールドも無い) なので、送信は「準備中」を明示する。
// 取得した PostItem は toPost / toReply で写すだけで、スレッド構造は推測しない。

import { Suspense, useMemo } from "react";
import { useSearchParams } from "next/navigation";
import { getJson } from "@/data/api";
import type { Group as GroupDTO, InstancePost, Me } from "@/data/dto";
import {
  usePagedResource,
  useResource,
  type ResourceState,
} from "@/data/hooks";
import { toGroup, toPost, toProfile, toReply, toUser } from "@/data/mappers";
import type { Group, Post, User } from "@/domain/types";
import { GroupReplyScreen } from "@/components/screens/GroupReplyScreen";

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
  );
  const {
    state: postsState,
    items,
    reload: reloadPosts,
  } = usePagedResource<InstancePost>(id ? `/instances/${id}/posts` : "", [id]);
  // composer のアバター表示に使う。送信は準備中だが、ui-v2 の見た目は保つ。
  const { state: meState } = useResource<Me>(
    (signal) => getJson<Me>("/me", { signal }),
    [],
  );

  const visible = useMemo(() => items.filter(isVisible), [items]);
  const posts = useMemo(() => visible.map(toPost), [visible]);
  // backend は reply を返さないので、開いている投稿を除いた一覧をそのまま返信として描画する。
  const replies = useMemo(
    () => visible.filter((item) => item.uri !== postId).map(toReply),
    [visible, postId],
  );
  const me: User = meState.status === "ok" ? toProfile(meState.data) : toUser(null);

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
