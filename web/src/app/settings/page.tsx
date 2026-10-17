"use client";

// /settings は static export なので SSR ではデータを取らない。サインイン後に
// useResource がクライアントでだけ GET /me を叩き、settings を toSettings で
// ドメインへ写す。

import { getJson } from "@/data/api";
import type { Me } from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toSettings } from "@/data/mappers";
import type { Settings } from "@/domain/types";
import { SettingsScreen } from "@/components/screens/SettingsScreen";

export default function SettingsPage() {
  const { state, reload } = useResource<Me>(
    (signal) => getJson<Me>("/me", { signal }),
    [],
  );

  // ok のときだけ DTO をドメイン Settings に写す。それ以外は状態をそのまま渡す。
  const settingsState: ResourceState<Settings> =
    state.status === "ok"
      ? { status: "ok", data: toSettings(state.data.settings) }
      : state;

  return <SettingsScreen state={settingsState} onRetry={reload} />;
}
