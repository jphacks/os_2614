"use client";

// /profile は static export なので SSR ではデータを取らない。サインイン後に
// useResource がクライアントでだけ GET /me を叩き、DTO を toProfile でドメインへ写す。
// ステータスは PUT /me/status (13 §1)、プロフィール (名前・自己紹介・画像) は
// PATCH /me/profile・PUT/DELETE /me/avatar・PUT/DELETE /me/banner (13 §1.3)、
// 公開投稿は ProfileScreen 内の PublicPostsSection が GET /feed?source=user:{did} (17 §3.2) で取る。

import { useState } from "react";
import {
  deleteJson,
  getJson,
  patchJson,
  putForm,
  putJson,
  toResult,
  type ResourceResult,
} from "@/data/api";
import type { Me } from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toProfile } from "@/data/mappers";
import type { Profile } from "@/domain/types";
import { ProfileScreen, type StatusSaveResult } from "@/components/screens";
import type {
  ProfileActionResult,
  ProfileUpdateInput,
} from "@/components/screens/ProfileScreen";

// 編集 API は GET /me と同じ平坦な形を返す (controller.meResponse)。返った Me は
// 「どの GET /me 結果を基にしたか」を key に持つ楽観値として適用し、server の取得結果が
// 変わったら捨てる (data/hooks.ts の keyed-state パターン)。reload せず表示を即時更新する。
function toActionResult(
  result: ResourceResult<Me>,
  apply: (me: Me) => void,
): ProfileActionResult {
  if (result.status === "ok") {
    apply(result.data);
    return { status: "ok" };
  }
  if (result.status === "unavailable") {
    return { status: "unavailable" };
  }
  return { status: "error", message: result.error.message };
}

export default function ProfilePage() {
  const { state, reload } = useResource<Me>(
    (signal) => getJson<Me>("/me", { signal }),
    [],
    { cacheKey: "/me" },
  );

  const serverKey = state.status === "ok" ? JSON.stringify(state.data) : "";
  const [applied, setApplied] = useState<{ key: string; me: Me } | null>(null);

  const applyMe = (me: Me) => {
    if (state.status === "ok") {
      setApplied({ key: JSON.stringify(state.data), me });
    }
  };

  // ok のときだけ DTO をドメイン Profile に写す。それ以外は状態をそのまま渡す。
  const effectiveMe: ResourceState<Me> =
    state.status === "ok" && applied && applied.key === serverKey
      ? { status: "ok", data: applied.me }
      : state;
  const meState: ResourceState<Profile> =
    effectiveMe.status === "ok"
      ? { status: "ok", data: toProfile(effectiveMe.data) }
      : effectiveMe;

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

  const saveProfile = async (
    input: ProfileUpdateInput,
  ): Promise<ProfileActionResult> =>
    toActionResult(
      await toResult(() =>
        patchJson<Me>("/me/profile", {
          displayName: input.displayName,
          description: input.description,
        }),
      ),
      applyMe,
    );

  const changeAvatar = async (file: File): Promise<ProfileActionResult> => {
    const form = new FormData();
    form.append("file", file);
    return toActionResult(
      await toResult(() => putForm<Me>("/me/avatar", form)),
      applyMe,
    );
  };

  const removeAvatar = async (): Promise<ProfileActionResult> =>
    toActionResult(
      await toResult(() => deleteJson<Me>("/me/avatar")),
      applyMe,
    );

  const changeBanner = async (file: File): Promise<ProfileActionResult> => {
    const form = new FormData();
    form.append("file", file);
    return toActionResult(
      await toResult(() => putForm<Me>("/me/banner", form)),
      applyMe,
    );
  };

  const removeBanner = async (): Promise<ProfileActionResult> =>
    toActionResult(
      await toResult(() => deleteJson<Me>("/me/banner")),
      applyMe,
    );

  return (
    // GET /memories (13 §2) は後続 todo。ここでは空配列でメモリーの空表示を出す。
    <ProfileScreen
      state={meState}
      memories={[]}
      onRetry={reload}
      onSaveStatus={saveStatus}
      onSaveProfile={saveProfile}
      onChangeAvatar={changeAvatar}
      onRemoveAvatar={removeAvatar}
      onChangeBanner={changeBanner}
      onRemoveBanner={removeBanner}
    />
  );
}
