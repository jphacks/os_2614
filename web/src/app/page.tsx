"use client";

// / (ちかく) は static export なので SSR ではデータを取らない。サインイン後に
// クライアントでだけ次を取り、自分を中心にした泡の配置にする。
// - GET /me (自分)、GET /party/current (自分に接して並べる)
// - GET /friends と GET /friends/locations (07 §4.2)、GET /encounters (06 §6)
// - GET /groups/nearby (09 §5)。中心はメンバーのときだけ GET /groups/{id} が返すので、
//   取れたものだけ方角とフレンドの縁の色に使う
// 参加/退出は POST /groups/{id}/join|leave。位置が古いとき backend は 403 location_stale を
// 返すので、その時だけ位置要求を出す。

import { useCallback, useEffect, useMemo, useState } from "react";
import { getJson, postJson } from "@/data/api";
import type {
  Encounter as EncounterDTO,
  Friend as FriendDTO,
  FriendLocation as FriendLocationDTO,
  Group as GroupDTO,
  GroupNearby as GroupNearbyDTO,
  Me,
  Party as PartyDTO,
} from "@/data/dto";
import { usePagedResource, useResource, type ResourceState } from "@/data/hooks";
import {
  toEncounter,
  toFriend,
  toFriendLocation,
  toGroupNearby,
  toParty,
  toProfile,
} from "@/data/mappers";
import type { FriendLocation, GroupNearby } from "@/domain/types";
import { ChainProximity } from "@/device/proximity";
import {
  useLocation,
  useLocationAccuracySettings,
  useReloadOnLocationUpload,
} from "@/device/proximity/hooks";
import { useBleNearbyFriends } from "@/device/proximity/party";
import { useCompassHeading } from "@/hooks/useCompassHeading";
import type { GroupArea } from "@/components/feature/nearby";
import { NearbyScreen } from "@/components/screens";

const LOCATION_INTERVAL_MS = 10_000;
/** これより精度が悪いときだけ Wi-Fi スキャンの案内を出す。屋外で GNSS が効いていれば出さない。 */
const WIFI_SCAN_HINT_ACCURACY_M = 30;
/** 中心を取りに行くグループの上限。近いグループが多すぎても詳細を叩きすぎない。 */
const MAX_GROUP_DETAILS = 12;

export default function NearbyPage() {
  const { state, reload } = useResource<GroupNearbyDTO>(
    (signal) => getJson<GroupNearbyDTO>("/groups/nearby", { signal }),
    [],
    { cacheKey: "/groups/nearby" },
  );
  const { state: meState } = useResource<Me>(
    (signal) => getJson<Me>("/me", { signal }),
    [],
    { cacheKey: "/me" },
  );
  const { state: partyState } = useResource<PartyDTO | null>(
    (signal) => getJson<PartyDTO | null>("/party/current", { signal }),
    [],
    { cacheKey: "/party/current" },
  );
  const { state: locationsState } = useResource<{ items: FriendLocationDTO[] }>(
    (signal) => getJson<{ items: FriendLocationDTO[] }>("/friends/locations", { signal }),
    [],
    { cacheKey: "/friends/locations" },
  );
  const { items: friendItems } = usePagedResource<FriendDTO>("/friends", []);
  const { items: encounterItems } = usePagedResource<EncounterDTO>("/encounters", []);
  const { fix } = useLocation({ intervalMs: LOCATION_INTERVAL_MS, enabled: true });
  const heading = useCompassHeading();
  const accuracySettings = useLocationAccuracySettings();
  const bleNearby = useBleNearbyFriends();
  const wifiScanHint =
    accuracySettings !== null &&
    accuracySettings.locationEnabled &&
    !accuracySettings.wifiScanAvailable &&
    (fix === null || fix.accuracyM > WIFI_SCAN_HINT_ACCURACY_M);

  useReloadOnLocationUpload(
    state.status === "error" && state.error.code === "location_stale",
    reload,
  );

  // 向きが変わるたびに再描画されるので、配置のやり直しを招かないよう写像は memo しておく
  const nearbyState: ResourceState<GroupNearby> = useMemo(
    () => (state.status === "ok" ? { status: "ok", data: toGroupNearby(state.data) } : state),
    [state],
  );
  const me = useMemo(() => (meState.status === "ok" ? toProfile(meState.data) : null), [meState]);
  const party = useMemo(
    () => (partyState.status === "ok" && partyState.data ? toParty(partyState.data) : null),
    [partyState],
  );
  const friends = useMemo(() => friendItems.map(toFriend), [friendItems]);
  const encounters = useMemo(() => encounterItems.map(toEncounter), [encounterItems]);

  const friendLocations = useMemo(() => {
    const map = new Map<string, FriendLocation>();
    if (locationsState.status === "ok") {
      for (const item of locationsState.data.items) {
        const location = toFriendLocation(item);
        map.set(location.did, location);
      }
    }
    return map;
  }, [locationsState]);

  // 位置は 10 秒ごとに更新されるが、配置は 1e-4 度 (約 10m) 未満の動きではやり直さない
  const lat = fix ? Math.round(fix.lat * 1e4) / 1e4 : null;
  const lon = fix ? Math.round(fix.lon * 1e4) / 1e4 : null;
  const myLocation = useMemo(
    () => (lat !== null && lon !== null ? { lat, lon } : null),
    [lat, lon],
  );

  const groupIds = useMemo(() => {
    if (nearbyState.status !== "ok") return "";
    return [...nearbyState.data.inside, ...nearbyState.data.near]
      .slice(0, MAX_GROUP_DETAILS)
      .map((group) => group.id)
      .join(",");
  }, [nearbyState]);

  const [groupAreas, setGroupAreas] = useState<ReadonlyMap<string, GroupArea>>(new Map());
  useEffect(() => {
    if (groupIds === "") return;
    const controller = new AbortController();
    void Promise.all(
      groupIds.split(",").map((id) =>
        getJson<GroupDTO>(`/groups/${encodeURIComponent(id)}`, { signal: controller.signal })
          .then((group) => ({ id, group }))
          .catch(() => null),
      ),
    ).then((results) => {
      if (controller.signal.aborted) return;
      const map = new Map<string, GroupArea>();
      for (const result of results) {
        const group = result?.group;
        if (
          result &&
          typeof group?.centerLat === "number" &&
          typeof group.centerLon === "number" &&
          typeof group.radiusM === "number"
        ) {
          map.set(result.id, { lat: group.centerLat, lon: group.centerLon, radiusM: group.radiusM });
        }
      }
      setGroupAreas(map);
    });
    return () => controller.abort();
  }, [groupIds]);

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

  const openLocationSettings = useCallback(() => {
    void ChainProximity.openLocationSettings().catch(() => undefined);
  }, []);

  return (
    <NearbyScreen
      me={me}
      myLocation={myLocation}
      heading={heading}
      friends={friends}
      friendLocations={friendLocations}
      encounters={encounters}
      party={party}
      groups={nearbyState}
      groupAreas={groupAreas}
      bleNearby={bleNearby}
      onJoin={join}
      onLeave={leave}
      onRetry={reload}
      onEnableLocation={enableLocation}
      onOpenLocationSettings={wifiScanHint ? openLocationSettings : undefined}
    />
  );
}
