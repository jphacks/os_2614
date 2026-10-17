"use client";

import { usePagedResource, type ResourceState } from "@/data/hooks";
import { toEncounter } from "@/data/mappers";
import type { Encounter as EncounterDTO } from "@/data/dto";
import type { Encounter } from "@/domain/types";
import { EncountersScreen } from "@/components/screens";

export default function EncountersPage() {
  const { state, items, hasMore, loadingMore, loadMore, reload } =
    usePagedResource<EncounterDTO>("/encounters", []);

  const encountersState: ResourceState<Encounter[]> =
    state.status === "ok" ? { status: "ok", data: items.map(toEncounter) } : state;

  return (
    <EncountersScreen
      state={encountersState}
      hasMore={hasMore}
      loadingMore={loadingMore}
      onLoadMore={loadMore}
      onRetry={reload}
    />
  );
}
