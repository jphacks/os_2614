"use client";

// /settings は static export なので SSR ではデータを取らない。サインイン後に
// useResource がクライアントでだけ GET /me を叩き、settings を toSettings で
// ドメインへ写す。エリア (07 §4.3) は /me/areas を別リソースとして取得する。

import {
  deleteJson,
  getJson,
  postJson,
  toResult,
  type ResourceResult,
} from "@/data/api";
import type { Area as AreaDTO, Me } from "@/data/dto";
import {
  usePagedResource,
  useResource,
  useSettingsPersistence,
  type ResourceState,
} from "@/data/hooks";
import { toArea, toSettings } from "@/data/mappers";
import { useLocation } from "@/device/proximity/hooks";
import type { Area, Settings } from "@/domain/types";
import type { AreaDraft, AreaMutationResult } from "@/components/feature/settings";
import { SettingsScreen, type AccountDeleteResult } from "@/components/screens";

const LOCATION_INTERVAL_MS = 5_000;

/** api() の分類結果をエリア操作の結果へ畳む。作成・削除はどちらも body を持たない。 */
function toMutation(result: ResourceResult<unknown>): AreaMutationResult {
  switch (result.status) {
    case "ok":
      return { status: "ok" };
    case "unavailable":
      return { status: "unavailable" };
    case "error":
      return { status: "error", message: result.error.message };
  }
}

export default function SettingsPage() {
  const { state, reload } = useResource<Me>(
    (signal) => getJson<Me>("/me", { signal }),
    [],
    { cacheKey: "/me" },
  );

  // ok のときだけ DTO をドメイン Settings に写す。それ以外は状態をそのまま渡す。
  const settingsState: ResourceState<Settings> =
    state.status === "ok"
      ? { status: "ok", data: toSettings(state.data.settings) }
      : state;

  // 楽観更新 + デバウンス保存 (PATCH /me/settings + PUT /me/location-sharing)。
  const save = useSettingsPersistence(settingsState);

  // エリア (07 §4.3)。作成の中心は現在地。
  const areas = usePagedResource<AreaDTO>("/me/areas", []);
  const { fix, error: locationError } = useLocation({
    intervalMs: LOCATION_INTERVAL_MS,
    enabled: true,
  });

  const createArea = async (draft: AreaDraft): Promise<AreaMutationResult> => {
    if (!fix) {
      return { status: "error", message: "現在地を取得できませんでした" };
    }
    const result = await toResult(() =>
      postJson<AreaDTO>("/me/areas", {
        label: draft.label,
        lat: fix.lat,
        lon: fix.lon,
        radiusM: draft.radiusM,
      }),
    );
    areas.reload();
    return toMutation(result);
  };

  const deleteArea = async (id: string): Promise<AreaMutationResult> => {
    const result = await toResult(() =>
      deleteJson<void>(`/me/areas/${encodeURIComponent(id)}`),
    );
    areas.reload();
    return toMutation(result);
  };

  const areaItems: Area[] = areas.items.map(toArea);

  const deleteAccount = async (opts: {
    deletePdsRecords: boolean;
  }): Promise<AccountDeleteResult> => {
    const result = await toResult(() =>
      deleteJson<void>("/me", { query: { deletePdsRecords: opts.deletePdsRecords } }),
    );
    switch (result.status) {
      case "ok":
        return { status: "ok" };
      case "unavailable":
        return { status: "unavailable" };
      case "error":
        return { status: "error", message: result.error.message };
    }
  };

  return (
    <SettingsScreen
      state={save.state}
      onChange={save.onChange}
      saving={save.saving}
      saveError={save.error}
      onRetrySave={save.retry}
      onRetry={reload}
      areas={{
        state: areas.state,
        items: areaItems,
        hasMore: areas.hasMore,
        loadingMore: areas.loadingMore,
        onLoadMore: areas.loadMore,
        location: fix ? { lat: fix.lat, lon: fix.lon } : null,
        locationError,
        onRetry: areas.reload,
        onCreate: createArea,
        onDelete: deleteArea,
      }}
      onDeleteAccount={deleteAccount}
    />
  );
}
