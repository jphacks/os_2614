"use client";

// /friends は static export なので SSR ではデータを取らない。サインイン後に
// usePagedResource がクライアントでだけ GET /friends (07 §2.3) を叩き、DTO を
// toFriend でドメインへ写す。DELETE /friends/{did} は画面の削除操作から、
// PUT /friends/{did}/location-share は相手ごとの見せ方の変更から呼ぶ。
// GET /friends/locations (07 §4.2) で、相手が自分に見せている現在位置を出す。

import { useCallback, useMemo } from "react";
import { deleteJson, getJson, putJson } from "@/data/api";
import type {
  Friend as FriendDTO,
  FriendLocation as FriendLocationDTO,
} from "@/data/dto";
import { usePagedResource, useResource, type ResourceState } from "@/data/hooks";
import { toFriend, toFriendLocation } from "@/data/mappers";
import type { Friend, FriendLocation, LocationPrecision } from "@/domain/types";
import { FriendsScreen } from "@/components/screens";

export default function FriendsPage() {
  const { state, items, hasMore, loadingMore, loadMore, reload } =
    usePagedResource<FriendDTO>("/friends", []);

  const { state: locationsState, reload: reloadLocations } = useResource<{
    items: FriendLocationDTO[];
  }>(
    (signal) =>
      getJson<{ items: FriendLocationDTO[] }>("/friends/locations", { signal }),
    [],
    { cacheKey: "/friends/locations" },
  );

  const locations = useMemo(() => {
    const map = new Map<string, FriendLocation>();
    if (locationsState.status === "ok") {
      for (const item of locationsState.data.items) {
        const location = toFriendLocation(item);
        map.set(location.did, location);
      }
    }
    return map;
  }, [locationsState]);

  // ok のときだけ DTO をドメイン Friend に写す。それ以外は状態をそのまま渡す。
  const friendsState: ResourceState<Friend[]> =
    state.status === "ok" ? { status: "ok", data: items.map(toFriend) } : state;

  // DELETE /friends/{did} (07 §2.3)。204 を返す。成功・失敗は画面が楽観更新を
  // 確定 / 巻き戻すため、ここでは結果 (reject) を返すだけにする。
  const removeFriend = useCallback(async (did: string): Promise<void> => {
    await deleteJson<void>(`/friends/${encodeURIComponent(did)}`);
  }, []);

  // PUT /friends/{did}/location-share (07 §4.2)。204 を返す。精度は相手ごと。
  const sharePrecision = useCallback(
    async (
      did: string,
      precision: LocationPrecision,
      areasOnly: boolean,
    ): Promise<void> => {
      await putJson<void>(`/friends/${encodeURIComponent(did)}/location-share`, {
        precision,
        areasOnly,
      });
      reload();
      reloadLocations();
    },
    [reload, reloadLocations],
  );

  return (
    <FriendsScreen
      state={friendsState}
      hasMore={hasMore}
      loadingMore={loadingMore}
      onLoadMore={loadMore}
      onDelete={removeFriend}
      locations={locations}
      locationsReady={locationsState.status === "ok"}
      onSharePrecision={sharePrecision}
      onRetry={reload}
    />
  );
}
