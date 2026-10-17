"use client";

// /group は static export なので SSR ではデータを取らない。`?id=` の group を
// Space とみなし (09 §1)、ヘッダーは GET /groups/{id} (09 §4)、タイムラインは
// GET /instances/{id}/posts (10 §4.2)、Pin は GET /instances/{id}/pins (10 §6) から
// クライアントでだけ取得する。DTO は toGroup / toPost / toPin でドメインへ写し、
// UI には DTO を渡さない。

import { Suspense, useCallback, useMemo, useState } from "react";
import { useSearchParams } from "next/navigation";
import { deleteJson, getJson, postJson, toResult } from "@/data/api";
import type {
  Group as GroupDTO,
  InstancePost,
  Pin as PinDTO,
} from "@/data/dto";
import {
  usePagedResource,
  useResource,
  type ResourceState,
} from "@/data/hooks";
import { toGroup, toPin, toPost } from "@/data/mappers";
import type { Group, Pin, Post } from "@/domain/types";
import { GroupScreen } from "@/components/screens/GroupScreen";
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
