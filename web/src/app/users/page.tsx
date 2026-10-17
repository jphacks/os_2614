"use client";

// /users は static export なので動的セグメントを使わず、対象は `?did=` で受ける (ui-v2 §1)。
// サインイン後に useResource がクライアントでだけ GET /users/{did} (13 §1) を叩き、
// DTO は ok のときだけ toProfile でドメインへ写す。公開投稿は UserProfileScreen 内の
// PublicPostsSection が GET /feed?source=user:{did} (17 §3.2) で取る。
// GET /users/{did} は統計を返さないため、この画面は counts を表示しない。

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { getJson } from "@/data/api";
import type { Me, Profile as ProfileDTO } from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toProfile } from "@/data/mappers";
import type { Profile } from "@/domain/types";
import { UserProfileScreen } from "@/components/screens/UserProfileScreen";

function UserProfile() {
  const did = useSearchParams().get("did") ?? "";

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
