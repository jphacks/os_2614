"use client";

// /group/invites は static export。`?id=` の closed グループの招待 (09 §6) と、
// 自分宛ての招待 (GET /group-invites) をクライアントでだけ取得する。

import { Suspense, useCallback } from "react";
import { useSearchParams } from "next/navigation";
import {
  deleteJson,
  getJson,
  postJson,
  toResult,
  type ResourceResult,
} from "@/data/api";
import type {
  GroupInvite as GroupInviteDTO,
  GroupInviteInboxItem as GroupInviteInboxItemDTO,
} from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toGroupInvite, toGroupInviteInboxItem } from "@/data/mappers";
import type { GroupInvite, GroupInviteInboxItem } from "@/domain/types";
import {
  GroupInvitesScreen,
  type GroupInviteActionResult,
} from "@/components/screens";

function actionResult(result: ResourceResult<unknown>): GroupInviteActionResult {
  if (result.status === "ok") {
    return { status: "ok" };
  }
  if (result.status === "unavailable") {
    return { status: "unavailable" };
  }
  return { status: "error", message: result.error.message };
}

function GroupInvites() {
  const id = useSearchParams().get("id") ?? "";

  const { state: membersState, reload: reloadMembers } = useResource<{
    items: GroupInviteDTO[];
  }>(
    (signal) =>
      id
        ? getJson<{ items: GroupInviteDTO[] }>(
            `/groups/${encodeURIComponent(id)}/invites`,
            { signal },
          )
        : Promise.resolve({ items: [] }),
    [id],
  );
  const { state: inboxState, reload: reloadInbox } = useResource<{
    items: GroupInviteInboxItemDTO[];
  }>(
    (signal) =>
      getJson<{ items: GroupInviteInboxItemDTO[] }>("/group-invites", { signal }),
    [],
  );

  const reloadAll = useCallback(() => {
    reloadMembers();
    reloadInbox();
  }, [reloadMembers, reloadInbox]);

  const onCreateInvite = useCallback(
    async (did: string): Promise<GroupInviteActionResult> => {
      const result = await toResult(() =>
        postJson<unknown>(`/groups/${encodeURIComponent(id)}/invites`, { did }),
      );
      if (result.status === "ok") {
        reloadMembers();
      }
      return actionResult(result);
    },
    [id, reloadMembers],
  );

  const onApprove = useCallback(
    async (inviteId: string): Promise<GroupInviteActionResult> => {
      const result = await toResult(() =>
        postJson<unknown>(
          `/groups/${encodeURIComponent(id)}/invites/${encodeURIComponent(inviteId)}/approve`,
          {},
        ),
      );
      if (result.status === "ok") {
        reloadMembers();
      }
      return actionResult(result);
    },
    [id, reloadMembers],
  );

  const onCancel = useCallback(
    async (inviteId: string): Promise<GroupInviteActionResult> => {
      const result = await toResult(() =>
        deleteJson<void>(
          `/groups/${encodeURIComponent(id)}/invites/${encodeURIComponent(inviteId)}`,
        ),
      );
      if (result.status === "ok") {
        reloadMembers();
      }
      return actionResult(result);
    },
    [id, reloadMembers],
  );

  const onAccept = useCallback(
    async (inviteId: string): Promise<GroupInviteActionResult> => {
      const result = await toResult(() =>
        postJson<unknown>(
          `/group-invites/${encodeURIComponent(inviteId)}/accept`,
          {},
        ),
      );
      if (result.status === "ok") {
        reloadInbox();
      }
      return actionResult(result);
    },
    [reloadInbox],
  );

  const onDecline = useCallback(
    async (inviteId: string): Promise<GroupInviteActionResult> => {
      const result = await toResult(() =>
        postJson<unknown>(
          `/group-invites/${encodeURIComponent(inviteId)}/decline`,
          {},
        ),
      );
      if (result.status === "ok") {
        reloadInbox();
      }
      return actionResult(result);
    },
    [reloadInbox],
  );

  const members: ResourceState<GroupInvite[]> | undefined =
    membersState.status === "ok"
      ? { status: "ok", data: membersState.data.items.map(toGroupInvite) }
      : membersState;
  const inbox: ResourceState<GroupInviteInboxItem[]> =
    inboxState.status === "ok"
      ? { status: "ok", data: inboxState.data.items.map(toGroupInviteInboxItem) }
      : inboxState;

  return (
    <GroupInvitesScreen
      groupId={id}
      members={id ? members : undefined}
      inbox={inbox}
      onCreateInvite={onCreateInvite}
      onApprove={onApprove}
      onCancel={onCancel}
      onAccept={onAccept}
      onDecline={onDecline}
      onRetry={reloadAll}
    />
  );
}

export default function GroupInvitesPage() {
  return (
    <Suspense fallback={null}>
      <GroupInvites />
    </Suspense>
  );
}
