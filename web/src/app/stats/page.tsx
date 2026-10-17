"use client";

// /stats は static export なので SSR ではデータを取らない。サインイン後に
// useResource がクライアントでだけ GET /me/stats を叩き、DTO を toStats でドメインへ写す。
// backend 未実装なら 404/405/501 が返り、StatsScreen が「準備中」を出す。

import { getJson } from "@/data/api";
import type { Stats as StatsDTO } from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toStats } from "@/data/mappers";
import type { Stats } from "@/domain/types";
import { StatsScreen } from "@/components/screens/StatsScreen";

export default function StatsPage() {
  const { state, reload } = useResource<StatsDTO>(
    (signal) => getJson<StatsDTO>("/me/stats", { signal }),
    [],
  );

  // ok のときだけ DTO をドメイン Stats に写す。それ以外は状態をそのまま渡す。
  const statsState: ResourceState<Stats> =
    state.status === "ok" ? { status: "ok", data: toStats(state.data) } : state;

  return <StatsScreen state={statsState} onRetry={reload} />;
}
