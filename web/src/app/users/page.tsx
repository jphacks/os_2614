"use client";

// /users は static export なので動的セグメントを使わず、対象は `?did=` で受ける (ui-v2 §1)。
// サインイン後に useResource がクライアントでだけ GET /users/{did} (13 §1) を叩き、
// DTO は ok のときだけ toProfile でドメインへ写す。公開投稿は UserProfileScreen 内の
// PublicPostsSection が GET /feed?source=user:{did} (17 §3.2) で取る。
// GET /users/{did} は統計を返さないため、この画面は counts を表示しない。

import { Suspense, useCallback, useState } from "react";
import { useSearchParams } from "next/navigation";
import {
  deleteJson,
  getJson,
  postJson,
  toResult,
  type ResourceResult,
} from "@/data/api";
import type { Me, Profile as ProfileDTO } from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toProfile } from "@/data/mappers";
import type { EncounterRelation, Profile } from "@/domain/types";
import {
  UserProfileScreen,
  type FriendRequestResult,
} from "@/components/screens/UserProfileScreen";

function parseRelation(value: string | null): EncounterRelation | undefined {
  return value === "none" ||
    value === "requested" ||
    value === "incoming" ||
    value === "friend"
    ? value
    : undefined;
}

function toFollowResult(result: ResourceResult<unknown>): FriendRequestResult {
  if (result.status === "ok") return { status: "ok" };
  if (result.status === "unavailable") return { status: "unavailable" };
  return { status: "error", message: result.error.message };
}

function UserProfile() {
  const params = useSearchParams();
  const did = params.get("did") ?? "";
  // すれ違い一覧から来たときだけ付く。申請の根拠 (07 §2.2 の viaRef) と現在の関係。
  const encounterId = params.get("encounter") ?? undefined;
  const initialRelation = parseRelation(params.get("relation"));
  const [sentDid, setSentDid] = useState<string | null>(null);
  const relation: EncounterRelation | undefined =
    sentDid === did ? "requested" : initialRelation;

  const onRequestFriend = useCallback(async (): Promise<FriendRequestResult> => {
    if (!encounterId) {
      return { status: "error", message: "すれ違いの情報がありません" };
    }
    const result = await toResult(() =>
      postJson<unknown>("/friends/requests", {
        did,
        via: "encounter",
        viaRef: encounterId,
      }),
    );
    if (result.status === "ok") {
      setSentDid(did);
      return { status: "ok" };
    }
    if (result.status === "unavailable") {
      return { status: "unavailable" };
    }
    return { status: "error", message: result.error.message };
  }, [did, encounterId]);

  // POST / DELETE /users/{did}/follow。どちらも 204。表示は画面が結果で切り替える。
  const onFollow = useCallback(
    async (): Promise<FriendRequestResult> =>
      toFollowResult(
        await toResult(() =>
          postJson<unknown>(`/users/${encodeURIComponent(did)}/follow`, {}),
        ),
      ),
    [did],
  );

  const onUnfollow = useCallback(
    async (): Promise<FriendRequestResult> =>
      toFollowResult(
        await toResult(() =>
          deleteJson<unknown>(`/users/${encodeURIComponent(did)}/follow`),
        ),
      ),
    [did],
  );

  const { state, reload } = useResource<ProfileDTO>(
    (signal) =>
      getJson<ProfileDTO>(`/users/${encodeURIComponent(did)}`, { signal }),
    [did],
    { cacheKey: "/users/:did" },
  );

  // 自分自身を開いたときだけ /profile/ への導線を出す。取得できなければ false。
  const { state: meState } = useResource<Me>(
    (signal) => getJson<Me>("/me", { signal }),
    [],
    { cacheKey: "/me" },
  );

  const profileState: ResourceState<Profile> =
    state.status === "ok"
      ? { status: "ok", data: toProfile(state.data) }
      : state;

  return (
    <UserProfileScreen
      did={did}
      state={profileState}
      isSelf={meState.status === "ok" && meState.data.did === did}
      relation={relation}
      onRequestFriend={encounterId ? onRequestFriend : undefined}
      onFollow={onFollow}
      onUnfollow={onUnfollow}
      onRetry={reload}
    />
  );
}

export default function UsersPage() {
  return (
    <Suspense fallback={null}>
      <UserProfile />
    </Suspense>
  );
}
