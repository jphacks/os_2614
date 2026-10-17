"use client";

// /profile は static export なので SSR ではデータを取らない。サインイン後に
// useResource がクライアントでだけ GET /me を叩き、DTO を toProfile でドメインへ写す。

import { getJson } from "@/data/api";
import type { Me } from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toProfile } from "@/data/mappers";
import type { Profile } from "@/domain/types";
import { ProfileScreen } from "@/components/screens/ProfileScreen";

export default function ProfilePage() {
  const { state, reload } = useResource<Me>(
    (signal) => getJson<Me>("/me", { signal }),
    [],
  );

  // ok のときだけ DTO をドメイン Profile に写す。それ以外は状態をそのまま渡す。
  const meState: ResourceState<Profile> =
    state.status === "ok"
      ? { status: "ok", data: toProfile(state.data) }
      : state;

  return (
    // GET /memories (13 §2) は後続 todo。ここでは空配列でメモリーの空表示を出す。
    <ProfileScreen state={meState} memories={[]} onRetry={reload} />
  );
}
