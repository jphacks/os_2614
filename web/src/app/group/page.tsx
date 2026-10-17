"use client";

// /group は static export なので SSR ではデータを取らない。`?id=` の group を
// Space とみなし (09 §1)、ヘッダーは GET /groups/{id} (09 §4)、タイムラインは
// GET /instances/{id}/posts (10 §4.2)、Pin は GET /instances/{id}/pins (10 §6) から
// クライアントでだけ取得する。DTO は toGroup / toPost / toPin でドメインへ写し、
// UI には DTO を渡さない。

import { Suspense, useCallback, useMemo, useState } from "react";
import { useSearchParams } from "next/navigation";
import { ApiError } from "@/auth/errors";
import {
  deleteJson,
  getJson,
  patchJson,
  postJson,
  toResult,
} from "@/data/api";
import type {
  Group as GroupDTO,
  GroupMember as GroupMemberDTO,
  InstancePost,
  Pin as PinDTO,
} from "@/data/dto";
import {
  usePagedResource,
  useResource,
  type ResourceState,
} from "@/data/hooks";
import { toGroup, toGroupMember, toPin, toPost } from "@/data/mappers";
import type { Group, GroupMember, Pin, Post } from "@/domain/types";
import {
  GroupScreen,
  type GroupSaveResult,
  type GroupUpdateDraft,
} from "@/components/screens/GroupScreen";
import {
  useRealtimeEvent,
  useRealtimeResync,
  useRealtimeSubscription,
} from "@/realtime/provider";

/** Pin の削除に使う rkey は at-uri の末尾セグメント (10 §6)。 */
function rkeyOf(uri: string): string {
  const parts = uri.split("/");
  return parts[parts.length - 1] ?? "";
}

/** 400 invalid_request の details.fields (backend) を行ごとのエラー文言に直す。 */
function readFieldErrors(error: ApiError): Record<string, string> | undefined {
  const raw = error.details?.fields;
  if (!raw || typeof raw !== "object") {
    return undefined;
  }
  const fields: Record<string, string> = {};
  for (const [key, value] of Object.entries(raw)) {
    if (typeof value === "string") {
      fields[key] = value;
    }
  }
  return Object.keys(fields).length > 0 ? fields : undefined;
}

/**
 * 論理削除 (hidden) の投稿と、moderation の表示指示が hide の投稿は出さない
 * (10 §4.2:80)。Pin 済みの投稿は pinned=true にして先頭へ寄せる。
 */
function toTimelinePosts(
  items: InstancePost[],
  pinnedSubjects: ReadonlySet<string>,
): Post[] {
  return items
    .filter((item) => !item.hidden && item.moderation?.display !== "hide")
    .map((item) => ({ ...toPost(item), pinned: pinnedSubjects.has(item.uri) }));
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
  const { state: pinsState, reload: reloadPins } = useResource<{
    items: PinDTO[];
  }>(
    (signal) => getJson<{ items: PinDTO[] }>(`/instances/${id}/pins`, { signal }),
    [id],
  );

  // メンバー一覧はメンバーだけが読める (09 §4)。owner は必ずメンバーなので、
  // 「非 owner かつ非メンバー」のときだけ取得しない。
  const canReadMembers =
    groupState.status === "ok" &&
    (groupState.data.isMember === true || groupState.data.role === "owner");
  const { state: membersState, reload: reloadMembers } = useResource<{
    items: GroupMemberDTO[];
  }>(
    (signal) =>
      canReadMembers
        ? getJson<{ items: GroupMemberDTO[] }>(
            `/groups/${encodeURIComponent(id)}/members`,
            { signal },
          )
        : Promise.resolve({ items: [] }),
    [id, canReadMembers],
  );

  const pins = useMemo(
    () => (pinsState.status === "ok" ? pinsState.data.items.map(toPin) : []),
    [pinsState],
  );
  const pinnedSubjects = useMemo(
    () => new Set(pins.map((pin) => pin.subject)),
    [pins],
  );
  const posts = useMemo(
    () => toTimelinePosts(items, pinnedSubjects),
    [items, pinnedSubjects],
  );

  const [pinBusy, setPinBusy] = useState(false);
  const [pinError, setPinError] = useState<string | null>(null);

  const pinPost = useCallback(
    async (post: Post) => {
      if (pinBusy) {
        return;
      }
      setPinBusy(true);
      setPinError(null);
      const result = await toResult(() =>
        postJson<PinDTO>(`/instances/${id}/pins`, { subject: post.id }),
      );
      setPinBusy(false);
      if (result.status !== "ok") {
        setPinError(
          result.status === "unavailable"
            ? "ピンはまだ利用できません"
            : result.error.message,
        );
        return;
      }
      // 楽観的に残さない。返った Pin は pin.created でも届くが、確実に REST で揃える。
      reloadPins();
    },
    [id, pinBusy, reloadPins],
  );

  const unpin = useCallback(
    async (pin: Pin) => {
      if (pinBusy) {
        return;
      }
      setPinBusy(true);
      setPinError(null);
      const result = await toResult(() =>
        deleteJson<void>(
          `/instances/${id}/pins/${encodeURIComponent(rkeyOf(pin.uri))}`,
        ),
      );
      setPinBusy(false);
      if (result.status !== "ok") {
        setPinError(
          result.status === "unavailable"
            ? "ピンはまだ利用できません"
            : result.error.message,
        );
        return;
      }
      reloadPins();
    },
    [id, pinBusy, reloadPins],
  );

  // PATCH /groups/{id} (09 §4)。geofence は変更できないので送らない。
  // 成功したら group とメンバーを取り直す (変更でタグや一覧が変わりうる)。
  const saveGroup = useCallback(
    async (draft: GroupUpdateDraft): Promise<GroupSaveResult> => {
      const result = await toResult(() =>
        patchJson<GroupDTO>(`/groups/${encodeURIComponent(id)}`, draft),
      );
      switch (result.status) {
        case "ok":
          reloadGroup();
          reloadMembers();
          return { status: "ok" };
        case "unavailable":
          return { status: "unavailable" };
        case "error":
          return {
            status: "error",
            message: result.error.message,
            fields: readFieldErrors(result.error),
          };
      }
    },
    [id, reloadGroup, reloadMembers],
  );

  // 表示中のグループ (Space) を購読し、リアルタイム更新を反映する (10 §7.2)。
  useRealtimeSubscription(id);
  // 再接続後は at-most-once の取りこぼしを補うためタイムラインと Pin を取り直す (10 §7.3)。
  useRealtimeResync(() => {
    reloadPosts();
    reloadPins();
  });
  useRealtimeEvent("post.created", (post) => {
    if (post.spaceId === id) {
      reloadPosts();
    }
  });
  useRealtimeEvent("pin.created", (_pin, frame) => {
    if (frame.space === id) {
      reloadPins();
    }
  });
  useRealtimeEvent("pin.deleted", (_data, frame) => {
    if (frame.space === id) {
      reloadPins();
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

  const pinResource: ResourceState<Pin[]> =
    pinsState.status === "ok" ? { status: "ok", data: pins } : pinsState;

  const members: GroupMember[] = useMemo(
    () =>
      membersState.status === "ok"
        ? membersState.data.items.map(toGroupMember)
        : [],
    [membersState],
  );
  const membersResource: ResourceState<GroupMember[]> =
    membersState.status === "ok"
      ? { status: "ok", data: members }
      : membersState;

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
      pins={pins}
      pinsState={pinResource}
      pinsRetry={reloadPins}
      pinBusy={pinBusy}
      pinError={pinError}
      onPin={(post) => void pinPost(post)}
      onUnpin={(pin) => void unpin(pin)}
      members={members}
      membersState={membersResource}
      membersRetry={reloadMembers}
      onSave={saveGroup}
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
