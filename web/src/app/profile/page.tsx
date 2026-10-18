"use client";

// /profile は static export なので SSR ではデータを取らない。サインイン後に
// useResource がクライアントでだけ GET /me とプロフィール関連のリソースを取得する。

import { useState } from "react";
import { useAuth } from "@/auth";
import {
  deleteJson,
  getJson,
  patchJson,
  putForm,
  putJson,
  toResult,
  type ResourceResult,
} from "@/data/api";
import type { Me, Memory as MemoryDTO, Stats as StatsDTO } from "@/data/dto";
import {
  overwriteResourceCache,
  usePagedResource,
  useResource,
  type ResourceState,
} from "@/data/hooks";
import { toMemory, toProfile, toStats } from "@/data/mappers";
import type { Memory, Profile, Stats } from "@/domain/types";
import {
  ProfileScreen,
  type ProfileActionResult,
  type ProfileDraft,
  type StatusSaveResult,
} from "@/components/screens";

function isReauthRequired(error: { status: number; code: string }): boolean {
  return (
    (error.status === 401 && error.code === "atproto_session_expired") ||
    (error.status === 403 && error.code === "atproto_reauth_required")
  );
}

function actionResult(
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
  return {
    status: "error",
    message: result.error.message,
    reauth: isReauthRequired(result.error),
    conflict: result.error.status === 409,
  };
}

function toProfileState(state: ResourceState<Me>): ResourceState<Profile> {
  switch (state.status) {
    case "loading":
      return { status: "loading" };
    case "empty":
      return { status: "empty" };
    case "error":
      return { status: "error", error: state.error };
    case "unavailable":
      return { status: "unavailable", error: state.error };
    case "ok":
      return { status: "ok", data: toProfile(state.data) };
  }
}

export default function ProfilePage() {
  const { login } = useAuth();
  const { state, reload } = useResource<Me>(
    (signal) => getJson<Me>("/me", { signal }),
    [],
    { cacheKey: "/me" },
  );
  const statsResource = useResource<StatsDTO>(
    (signal) => getJson<StatsDTO>("/me/stats", { signal }),
    [],
    { cacheKey: "/me/stats" },
  );
  const memoriesResource = usePagedResource<MemoryDTO>("/memories", [], {
    limit: 6,
  });
  const [applied, setApplied] = useState<{ key: string; me: Me } | null>(null);
  const serverKey = state.status === "ok" ? JSON.stringify(state.data) : "";

  const applyMe = (me: Me) => {
    overwriteResourceCache(me.did, "/me", [], me);
    setApplied({ key: serverKey, me });
  };

  const effectiveMe =
    state.status === "ok" && applied?.key === serverKey
      ? applied.me
      : state.status === "ok"
        ? state.data
        : undefined;
  const meState: ResourceState<Profile> = effectiveMe
    ? { status: "ok", data: toProfile(effectiveMe) }
    : toProfileState(state);
  const stats: ResourceState<Stats> =
    statsResource.state.status === "ok"
      ? { status: "ok", data: toStats(statsResource.state.data) }
      : statsResource.state;
  const memories: ResourceState<Memory[]> =
    memoriesResource.state.status === "ok"
      ? {
          status: "ok",
          data: memoriesResource.items.slice(0, 6).map(toMemory),
        }
      : memoriesResource.state;

  const saveStatus = async (value: string): Promise<StatusSaveResult> => {
    const result = await toResult(() =>
      putJson<{ status: string }>("/me/status", { status: value.trim() }),
    );
    if (result.status === "ok") {
      if (effectiveMe) {
        applyMe({ ...effectiveMe, status: result.data.status ?? value.trim() });
      }
      return {
        status: "ok",
        value: result.data.status ?? value.trim(),
      };
    }
    if (result.status === "unavailable") {
      return { status: "unavailable" };
    }
    return {
      status: "error",
      message: result.error.message,
      reauth: isReauthRequired(result.error),
    };
  };

  const saveProfile = async (
    draft: ProfileDraft,
  ): Promise<ProfileActionResult> => {
    let remaining = { ...draft };
    const profileBody: { displayName?: string; description?: string } = {};
    if (draft.displayName.trim() !== draft.initialDisplayName.trim()) {
      profileBody.displayName = draft.displayName.trim();
    }
    if (draft.description.trim() !== draft.initialDescription.trim()) {
      profileBody.description = draft.description.trim();
    }
    const steps: {
      label: string;
      changed: boolean;
      run: () => Promise<ResourceResult<Me>>;
    }[] = [
      {
        label: "プロフィール",
        changed:
          effectiveMe !== undefined &&
          (draft.displayName.trim() !== draft.initialDisplayName.trim() ||
            draft.description.trim() !== draft.initialDescription.trim()),
        run: () =>
          toResult(() =>
            patchJson<Me>("/me/profile", profileBody),
          ),
      },
      {
        label: "アイコン",
        changed: draft.avatar.kind !== "keep",
        run: () => {
          if (draft.avatar.kind === "set") {
            const form = new FormData();
            form.append("file", draft.avatar.blob, "avatar.jpg");
            return toResult(() => putForm<Me>("/me/avatar", form));
          }
          return toResult(() => deleteJson<Me>("/me/avatar"));
        },
      },
      {
        label: "バナー",
        changed: draft.banner.kind !== "keep",
        run: () => {
          if (draft.banner.kind === "set") {
            const form = new FormData();
            form.append("file", draft.banner.blob, "banner.jpg");
            return toResult(() => putForm<Me>("/me/banner", form));
          }
          return toResult(() => deleteJson<Me>("/me/banner"));
        },
      },
    ];

    for (const step of steps) {
      if (!step.changed) {
        continue;
      }
      const result = await step.run();
      const handled = actionResult(result, applyMe);
      if (handled.status !== "ok") {
        if (handled.status === "error" && handled.conflict) {
          await reload();
          return {
            ...handled,
            message:
              "他の場所で更新されたため、最新の内容を読み込みました。もう一度保存してください",
            remaining,
          };
        }
        if (handled.status === "error") {
          return {
            ...handled,
            message: `${step.label}の保存に失敗しました: ${handled.message}`,
            remaining,
          };
        }
        return handled;
      }
      if (step.label === "アイコン") {
        remaining = { ...remaining, avatar: { kind: "keep" } };
      }
      if (step.label === "バナー") {
        remaining = { ...remaining, banner: { kind: "keep" } };
      }
      if (step.label === "プロフィール") {
        remaining = {
          ...remaining,
          initialDisplayName: draft.displayName.trim(),
          initialDescription: draft.description.trim(),
        };
      }
    }
    return { status: "ok" };
  };

  return (
    <ProfileScreen
      state={meState}
      stats={stats}
      memories={memories}
      onRetry={reload}
      onSaveStatus={saveStatus}
      onSaveProfile={saveProfile}
      onRelogin={() => void login()}
    />
  );
}
