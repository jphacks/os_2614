"use client";

// /settings/data は static export なので SSR ではデータを取らない。サインイン後に
// useResource がクライアントでだけ GET /me/data-map を叩く (O4、19 §7.1)。

import { getJson } from "@/data/api";
import type { DataMap as DataMapDTO } from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toDataMap } from "@/data/mappers";
import type { DataMap } from "@/domain/types";
import { DataMapScreen } from "@/components/screens";

export default function DataMapPage() {
  const { state, reload } = useResource<DataMapDTO>(
    (signal) => getJson<DataMapDTO>("/me/data-map", { signal }),
    [],
    { cacheKey: "/me/data-map" },
  );
  const mapState: ResourceState<DataMap> =
    state.status === "ok" ? { status: "ok", data: toDataMap(state.data) } : state;

  return <DataMapScreen state={mapState} onRetry={reload} />;
}
