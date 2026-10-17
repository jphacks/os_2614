"use client";

// /friends は static export なので SSR ではデータを取らない。サインイン後に
// usePagedResource がクライアントでだけ GET /friends (07 §2.3) を叩き、DTO を
// toFriend でドメインへ写す。DELETE /friends/{did} は画面の削除操作から呼ぶ。

import { useCallback } from "react";
import { deleteJson } from "@/data/api";
import type { Friend as FriendDTO } from "@/data/dto";
import { usePagedResource, type ResourceState } from "@/data/hooks";
import { toFriend } from "@/data/mappers";
import type { Friend } from "@/domain/types";
import { FriendsScreen } from "@/components/screens/FriendsScreen";

export default function FriendsPage() {
  const { state, items, hasMore, loadingMore, loadMore, reload } =
    usePagedResource<FriendDTO>("/friends", []);

  // ok のときだけ DTO をドメイン Friend に写す。それ以外は状態をそのまま渡す。
  const friendsState: ResourceState<Friend[]> =
    state.status === "ok" ? { status: "ok", data: items.map(toFriend) } : state;

  // DELETE /friends/{did} (07 §2.3)。204 を返す。成功・失敗は画面が楽観更新を
  // 確定 / 巻き戻すため、ここでは結果 (reject) を返すだけにする。
  const removeFriend = useCallback(async (did: string): Promise<void> => {
    await deleteJson<void>(`/friends/${encodeURIComponent(did)}`);
  }, []);

  return (
    <FriendsScreen
      state={friendsState}
      hasMore={hasMore}
      loadingMore={loadingMore}
      onLoadMore={loadMore}
      onDelete={removeFriend}
      onRetry={reload}
    />
  );
}
