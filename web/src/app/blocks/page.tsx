"use client";

import { useCallback } from "react";
import { ApiError } from "@/auth/errors";
import { deleteJson, postJson, toResult } from "@/data/api";
import type { User as UserDTO } from "@/data/dto";
import { usePagedResource, type ResourceState } from "@/data/hooks";
import { toUser } from "@/data/mappers";
import type { Block } from "@/domain/types";
import { BlocksScreen, type BlockAddResult } from "@/components/screens";

export default function BlocksPage() {
  const { state, items, reload } = usePagedResource<UserDTO>("/blocks", []);

  const add = useCallback(
    async (did: string): Promise<BlockAddResult> => {
      const result = await toResult(() => postJson<void>("/blocks", { did }));
      switch (result.status) {
        case "ok":
          reload();
          return { status: "ok" };
        case "unavailable":
          return { status: "unavailable" };
        case "error":
          if (result.error instanceof ApiError && result.error.status === 409) {
            return { status: "conflict" };
          }
          return { status: "error", message: result.error.message };
      }
    },
    [reload],
  );

  const remove = useCallback(
    async (did: string) => {
      await deleteJson<void>(`/blocks/${encodeURIComponent(did)}`);
      reload();
    },
    [reload],
  );

  const blocksState: ResourceState<Block[]> =
    state.status === "ok" ? { status: "ok", data: items.map(toUser) } : state;

  return (
    <BlocksScreen
      state={blocksState}
      onAdd={add}
      onRemove={remove}
      onRetry={reload}
    />
  );
}
