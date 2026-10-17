"use client";

// /group/votes は static export。`?id=` のグループ (Space) の投票 (09 §8.6) を
// クライアントでだけ取得する。発議・投票の後は一覧を取り直す (楽観状態は持たない)。

import { Suspense, useCallback } from "react";
import { useSearchParams } from "next/navigation";
import { getJson, postJson, toResult, type ResourceResult } from "@/data/api";
import type { GroupVoteList as GroupVoteListDTO } from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toGroupVote } from "@/data/mappers";
import type { GroupVote, GroupVoteKind } from "@/domain/types";
import {
  GroupVotesScreen,
  type GroupVoteActionResult,
} from "@/components/screens/GroupVotesScreen";

function actionResult(result: ResourceResult<unknown>): GroupVoteActionResult {
  if (result.status === "ok") {
    return { status: "ok" };
  }
  if (result.status === "unavailable") {
    return { status: "unavailable" };
  }
  return { status: "error", message: result.error.message };
}

function GroupVotes() {
  const id = useSearchParams().get("id") ?? "";

  const { state, reload } = useResource<GroupVoteListDTO>(
    (signal) =>
      id
        ? getJson<GroupVoteListDTO>(
            `/groups/${encodeURIComponent(id)}/votes`,
            { signal },
          )
        : Promise.resolve({ items: [] }),
    [id],
    { isEmpty: (data) => data.items.length === 0 },
  );

  const onPropose = useCallback(
    async (kind: GroupVoteKind): Promise<GroupVoteActionResult> => {
      const result = await toResult(() =>
        postJson<unknown>(`/groups/${encodeURIComponent(id)}/votes`, { kind }),
      );
      if (result.status === "ok") {
        reload();
      }
      return actionResult(result);
    },
    [id, reload],
  );

  const onBallot = useCallback(
    async (voteId: string, yes: boolean): Promise<GroupVoteActionResult> => {
      const result = await toResult(() =>
        postJson<unknown>(
          `/groups/${encodeURIComponent(id)}/votes/${encodeURIComponent(voteId)}/ballot`,
          { yes },
        ),
      );
      if (result.status === "ok") {
        reload();
      }
      return actionResult(result);
    },
    [id, reload],
  );

  const votes: ResourceState<GroupVote[]> =
    state.status === "ok"
      ? { status: "ok", data: state.data.items.map(toGroupVote) }
      : state;

  return (
    <GroupVotesScreen
      groupId={id}
      votes={votes}
      onPropose={onPropose}
      onBallot={onBallot}
      onRetry={reload}
    />
  );
}

export default function GroupVotesPage() {
  // useSearchParams は Suspense 境界の内側でだけ使える (static export の prerender 用)。
  return (
    <Suspense fallback={null}>
      <GroupVotes />
    </Suspense>
  );
}
