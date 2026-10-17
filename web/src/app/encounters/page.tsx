"use client";

import { usePagedResource, type ResourceState } from "@/data/hooks";
import { toEncounter } from "@/data/mappers";
import type { Encounter as EncounterDTO } from "@/data/dto";
import type { Encounter } from "@/domain/types";
import { useRealtimeResync } from "@/realtime/provider";
import { useRealtimeCache } from "@/realtime/store";
import { EncountersScreen } from "@/components/screens";

/** リアルタイムで届いたすれ違いを先頭側に重ね、id が同じものは二重に出さない。 */
function mergeEncounters(
  realtime: Encounter[],
  rest: Encounter[],
): Encounter[] {
  const seen = new Set(realtime.map((item) => item.id));
  return [...realtime, ...rest.filter((item) => !seen.has(item.id))];
}

export default function EncountersPage() {
  const { state, items, hasMore, loadingMore, loadMore, reload } =
    usePagedResource<EncounterDTO>("/encounters", []);
  const cache = useRealtimeCache();

  // 再接続後は at-most-once の取りこぼしを補うため REST を取り直す (10 §7.3)。
  useRealtimeResync(reload);

  const encountersState: ResourceState<Encounter[]> =
    state.status === "ok"
      ? {
          status: "ok",
          data: mergeEncounters(
            cache.encounters.map(toEncounter),
            items.map(toEncounter),
          ),
        }
      : state;

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
