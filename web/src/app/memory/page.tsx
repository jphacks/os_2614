"use client";

// /memory は static export なので SSR ではデータを取らない。サインイン後に
// usePagedResource がクライアントでだけ GET /memories (13 §2) を叩き、
// DTO を toMemory でドメインへ写す。archived Space の一覧がここに出る。

import { deleteJson, toResult } from "@/data/api";
import type { Memory as MemoryDTO } from "@/data/dto";
import { usePagedResource, type ResourceState } from "@/data/hooks";
import { toMemory } from "@/data/mappers";
import type { Memory } from "@/domain/types";
import {
  MemoryScreen,
  type MemoryHideResult,
} from "@/components/screens/MemoryScreen";

export default function MemoryPage() {
  const { state, items, hasMore, loadingMore, loadMore, reload } =
    usePagedResource<MemoryDTO>("/memories", []);

  // ok のときだけ DTO をドメイン Memory に写す。それ以外は状態をそのまま渡す。
  const memoryState: ResourceState<Memory[]> =
    state.status === "ok"
      ? { status: "ok", data: state.data.map(toMemory) }
      : state;
  const memories = items.map(toMemory);

  // DELETE /memories/{spaceId} は 404/405/501 だけを unavailable に畳む。
  const hideMemory = async (spaceId: string): Promise<MemoryHideResult> => {
    const result = await toResult(() =>
      deleteJson<unknown>(`/memories/${spaceId}`),
    );
    if (result.status === "ok") {
      return "ok";
    }
    return result.status === "unavailable" ? "unavailable" : "error";
  };

  return (
    <MemoryScreen
      state={memoryState}
      memories={memories}
      hasMore={hasMore}
      loadingMore={loadingMore}
      onLoadMore={loadMore}
      onRetry={reload}
      onHide={hideMemory}
    />
  );
}
