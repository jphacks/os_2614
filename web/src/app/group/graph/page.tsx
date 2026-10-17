"use client";

// /group/graph は static export。サインイン後に GET /groups/graph (09 §7) を
// クライアントでだけ取得し、ノード / 辺を非地図のグラフとして描く。

import { getJson } from "@/data/api";
import type { GroupGraph as GroupGraphDTO } from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { useReloadOnLocationUpload } from "@/device/proximity/hooks";
import { toGroupGraph } from "@/data/mappers";
import type { GroupGraph } from "@/domain/types";
import { GroupGraphScreen } from "@/components/screens";

export default function GroupGraphPage() {
  const { state, reload } = useResource<GroupGraphDTO>(
    (signal) => getJson<GroupGraphDTO>("/groups/graph", { signal }),
    [],
    { isEmpty: (graph) => (graph.nodes ?? []).length === 0, cacheKey: "/groups/graph" },
  );

  useReloadOnLocationUpload(
    state.status === "error" && state.error.code === "location_stale",
    reload,
  );

  const resource: ResourceState<GroupGraph> =
    state.status === "ok"
      ? { status: "ok", data: toGroupGraph(state.data) }
      : state;

  return <GroupGraphScreen state={resource} onRetry={reload} />;
}
