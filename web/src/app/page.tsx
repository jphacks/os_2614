"use client";

// / (ちかく) は static export なので SSR ではデータを取らない。サインイン後に
// useResource がクライアントでだけ GET /groups/nearby (09 §5) を叩き、DTO を
// toGroupNearby でドメインへ写す。参加/退出は POST /groups/{id}/join|leave。
// 位置が古いとき backend は 403 location_stale を返すので、その時だけ位置要求を出す。

import { useCallback } from "react";
import { getJson, postJson } from "@/data/api";
import type { GroupNearby as GroupNearbyDTO } from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toGroupNearby } from "@/data/mappers";
import type { GroupNearby } from "@/domain/types";
import { ChainProximity } from "@/device/proximity";
import { useProximityStatus, useReloadOnLocationUpload } from "@/device/proximity/hooks";
import { NearbyScreen } from "@/components/screens";

export default function NearbyPage() {
  const { state, reload } = useResource<GroupNearbyDTO>(
    (signal) => getJson<GroupNearbyDTO>("/groups/nearby", { signal }),
    [],
    { cacheKey: "/groups/nearby" },
  );
  const proximity = useProximityStatus();

  useReloadOnLocationUpload(
    state.status === "error" && state.error.code === "location_stale",
    reload,
  );

  const nearbyState: ResourceState<GroupNearby> =
    state.status === "ok"
      ? { status: "ok", data: toGroupNearby(state.data) }
      : state;

  const join = useCallback(
    async (id: string) => {
      await postJson<void>(`/groups/${encodeURIComponent(id)}/join`, {});
      reload();
    },
    [reload],
  );

  const leave = useCallback(
    async (id: string) => {
      await postJson<void>(`/groups/${encodeURIComponent(id)}/leave`, {});
      reload();
    },
    [reload],
  );

  // 位置が古い (403 location_stale) ときの位置要求。権限要求の結果に関わらず再取得する。
  const enableLocation = useCallback(() => {
    void ChainProximity.requestPermissions()
      .catch(() => undefined)
      .finally(() => reload());
  }, [reload]);

  return (
    <NearbyScreen
      state={nearbyState}
      proximity={proximity}
      onJoin={join}
      onLeave={leave}
      onRetry={reload}
      onEnableLocation={enableLocation}
    />
  );
}

