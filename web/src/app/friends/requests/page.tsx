"use client";

import { useCallback, useMemo } from "react";
import { ApiError } from "@/auth/errors";
import {
  deleteJson,
  getJson,
  postJson,
  toResult,
  type ResourceResult,
} from "@/data/api";
import type {
  FriendRequest as FriendRequestDTO,
  Invite as InviteDTO,
  Page,
} from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toFriendRequest, toInvite, toUser } from "@/data/mappers";
import type { FriendRequest } from "@/domain/types";
import { useRealtimeEvent, useRealtimeResync } from "@/realtime/provider";
import { useRealtimeCache } from "@/realtime/store";
import {
  FriendRequestsScreen,
  type FriendActionResult,
  type InviteCreateResult,
} from "@/components/screens";

function actionResult(result: ResourceResult<unknown>): FriendActionResult {
  if (result.status === "ok") {
    return { status: "ok" };
  }
  if (result.status === "unavailable") {
    return { status: "unavailable" };
  }
  return { status: "error", message: result.error.message };
}

/** リアルタイムで届いた申請を先頭側に重ね、相手 DID が同じものは二重に出さない。 */
function mergeIncoming(
  realtime: FriendRequest[],
  rest: FriendRequest[],
): FriendRequest[] {
  const dids = new Set(realtime.map((item) => item.peer.did));
  return [...realtime, ...rest.filter((item) => !dids.has(item.peer.did))];
}

export default function FriendRequestsPage() {
  const {
    state: incomingState,
    reload: reloadIncoming,
  } = useResource<Page<FriendRequestDTO>>(
    (signal) => getJson<Page<FriendRequestDTO>>("/friends/requests", { query: { box: "incoming" }, signal }),
    [],
    { cacheKey: "/friends/requests?box=incoming" },
  );
  const {
    state: outgoingState,
    reload: reloadOutgoing,
  } = useResource<Page<FriendRequestDTO>>(
    (signal) => getJson<Page<FriendRequestDTO>>("/friends/requests", { query: { box: "outgoing" }, signal }),
    [],
    { cacheKey: "/friends/requests?box=outgoing" },
  );
  const cache = useRealtimeCache();

  const reloadAll = useCallback(() => {
    reloadIncoming();
    reloadOutgoing();
  }, [reloadIncoming, reloadOutgoing]);

  // `friend.request` は payload の相手をそのまま一覧へ重ねる (10 §7.3)。
  const realtimeIncoming = useMemo<FriendRequest[]>(
    () =>
      cache.friendEvents
        .filter((event) => event.kind === "request")
        .map((event) => ({
          id: `rt:${event.user.did}`,
          peer: toUser(event.user),
          via: "realtime",
          status: "pending",
          createdAt: "",
        })),
    [cache.friendEvents],
  );

  // 承認 / 変更は一覧の中身が変わるので取り直す。再接続後の取りこぼし補填も兼ねる (10 §7.3)。
  useRealtimeEvent("friend.accepted", reloadAll);
  useRealtimeEvent("friend.changed", reloadAll);
  useRealtimeResync(reloadAll);

  const respond = useCallback(
    async (id: string, action: "accept" | "decline"): Promise<FriendActionResult> => {
      const result = await toResult(() =>
        postJson<unknown>(`/friends/requests/${encodeURIComponent(id)}/${action}`, {}),
      );
      if (result.status === "ok") {
        reloadAll();
      }
      return actionResult(result);
    },
    [reloadAll],
  );

  const onAccept = useCallback(
    (id: string) => respond(id, "accept"),
    [respond],
  );
  const onDecline = useCallback(
    (id: string) => respond(id, "decline"),
    [respond],
  );

  const onCancel = useCallback(
    async (id: string): Promise<FriendActionResult> => {
      const result = await toResult(() =>
        deleteJson<void>(`/friends/requests/${encodeURIComponent(id)}`),
      );
      if (result.status === "ok") {
        reloadAll();
      }
      return actionResult(result);
    },
    [reloadAll],
  );

  const onCreateInvite = useCallback(async (): Promise<InviteCreateResult> => {
    const result = await toResult(() =>
      postJson<InviteDTO>("/friends/invites", {}),
    );
    if (result.status === "ok") {
      return { status: "ok", invite: toInvite(result.data) };
    }
    if (result.status === "unavailable") {
      return { status: "unavailable" };
    }
    return { status: "error", message: result.error.message };
  }, []);

  const onAcceptInvite = useCallback(
    async (code: string): Promise<FriendActionResult> => {
      const result = await toResult(() =>
        postJson<unknown>(
          `/friends/invites/${encodeURIComponent(code)}/accept`,
          {},
        ),
      );
      if (result.status === "ok") {
        reloadAll();
        return { status: "ok" };
      }
      if (result.status === "unavailable") {
        return { status: "unavailable" };
      }
      if (result.error instanceof ApiError && result.error.status === 403) {
        return { status: "error", message: "この相手とは友達になれません。" };
      }
      return { status: "error", message: result.error.message };
    },
    [reloadAll],
  );

  const incoming: ResourceState<FriendRequest[]> =
    incomingState.status === "ok"
      ? {
          status: "ok",
          data: mergeIncoming(
            realtimeIncoming,
            incomingState.data.items.map(toFriendRequest),
          ),
        }
      : incomingState;
  const outgoing: ResourceState<FriendRequest[]> =
    outgoingState.status === "ok"
      ? { status: "ok", data: outgoingState.data.items.map(toFriendRequest) }
      : outgoingState;

  return (
    <FriendRequestsScreen
      incoming={incoming}
      outgoing={outgoing}
      onAccept={onAccept}
      onDecline={onDecline}
      onCancel={onCancel}
      onCreateInvite={onCreateInvite}
      onAcceptInvite={onAcceptInvite}
      onRetry={reloadAll}
    />
  );
}
