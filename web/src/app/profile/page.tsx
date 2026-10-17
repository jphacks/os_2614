"use client";

// /profile は static export なので SSR ではデータを取らない。サインイン後に
// useResource がクライアントでだけ GET /me を叩き、DTO を toProfile でドメインへ写す。
// ステータスは PUT /me/status (13 §1)、公開投稿は ProfileScreen 内の
// PublicPostsSection が GET /feed?source=user:{did} (17 §3.2) で取る。

import { getJson, putJson, toResult } from "@/data/api";
import type { Me } from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toProfile } from "@/data/mappers";
import type { Profile } from "@/domain/types";
import { ProfileScreen, type StatusSaveResult } from "@/components/screens";

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

  const saveStatus = async (status: string): Promise<StatusSaveResult> => {
    const result = await toResult(() =>
      putJson<{ status: string }>("/me/status", { status }),
    );
    switch (result.status) {
      case "ok":
        return { status: "ok", value: result.data.status ?? status };
      case "unavailable":
        return { status: "unavailable" };
      case "error":
        return { status: "error", message: result.error.message };
    }
  };

  return (
    // GET /memories (13 §2) は後続 todo。ここでは空配列でメモリーの空表示を出す。
    <ProfileScreen
      state={meState}
      memories={[]}
      onRetry={reload}
      onSaveStatus={saveStatus}
    />
  );
}
