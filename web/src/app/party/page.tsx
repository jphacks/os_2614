"use client";

// /party は static export なので SSR ではデータを取らない。サインイン後に useResource が
// GET /party/current を叩き、DTO を toParty でドメインへ写す。null は「所属パーティ無し」。
// `party.updated` (10 §7.3) はキャッシュに溜まり、ここで取得結果へ重ねて反映する。

import { useAuth } from "@/auth";
import {
  fetchPage,
  getJson,
  patchJson,
  postJson,
  toResult,
  type ResourceResult,
} from "@/data/api";
import type { InstancePost, Party as PartyDTO } from "@/data/dto";
import { useResource, type ResourceState } from "@/data/hooks";
import { toMedia, toParty } from "@/data/mappers";
import type { Media, Party } from "@/domain/types";
import type { PartyUpdatedData } from "@/realtime/events";
import { useRealtimeResync } from "@/realtime/provider";
import { useRealtimeCache } from "@/realtime/store";
import { PartyScreen, type PartyActionResult } from "@/components/screens/PartyScreen";

function classify(result: ResourceResult<unknown>): PartyActionResult {
  return result.status === "ok" ? { status: "ok" } : { status: result.status };
}

/** リアルタイムの party.updated を、取得済みの party へ重ねる (phase / 固定 / 在席)。 */
function withRealtimeUpdate(party: Party, update: PartyUpdatedData): Party {
  const present = new Set(update.present);
  return {
    ...party,
    phase: update.phase,
    locked: update.locked,
    members: party.members.map((member) => ({
      ...member,
      present: present.has(member.did),
    })),
  };
}

export default function PartyPage() {
  const { state: authState } = useAuth();
  const { state, reload } = useResource<PartyDTO | null>(
    (signal) => getJson<PartyDTO | null>("/party/current", { signal }),
    [],
    { cacheKey: "/party/current" },
  );
  const cache = useRealtimeCache();
  const realtime = cache.party;

  // 再接続後は at-most-once の取りこぼしを補うため REST を取り直す (10 §7.3)。
  useRealtimeResync(reload);

  // ok のときだけ DTO をドメイン Party に写し、リアルタイム更新を重ねる。
  // null は empty、それ以外は状態をそのまま渡す。
  const partyState: ResourceState<Party> =
    state.status === "ok"
      ? state.data === null
        ? { status: "empty" }
        : {
            status: "ok",
            data: realtime
              ? withRealtimeUpdate(toParty(state.data), realtime)
              : toParty(state.data),
          }
      : state;

  const party = partyState.status === "ok" ? partyState.data : null;

  // 写真は party の Space に投稿された media を集める（08 §5 / 10 §4）。
  const photos = useResource<Media[]>(
    async (signal) => {
      if (!party) {
        return [];
      }
      const page = await fetchPage<InstancePost>(
        `/instances/${party.id}/posts`,
        { signal },
      );
      return page.items.flatMap((post) => (post.media ?? []).map(toMedia));
    },
    [party?.id],
    { cacheKey: "/party/current/photos" },
  );

  const handleLeave = async (target: Party): Promise<PartyActionResult> => {
    const result = await toResult(() =>
      postJson<undefined>(`/parties/${target.id}/leave`, undefined),
    );
    if (result.status === "ok") {
      reload();
    }
    return classify(result);
  };

  const handleToggleLock = async (
    target: Party,
    locked: boolean,
  ): Promise<PartyActionResult> => {
    // lock/unlock は 08 §4.4 の P1。backend 未実装なら 404 が返り「準備中」へ倒す。
    const action = locked ? "lock" : "unlock";
    const result = await toResult(() =>
      postJson<void>(`/parties/${target.id}/${action}`, undefined),
    );
    return classify(result);
  };

  const handleRename = async (
    target: Party,
    name: string,
  ): Promise<PartyActionResult> => {
    const result = await toResult(() =>
      patchJson<PartyDTO>(`/parties/${target.id}`, { name }),
    );
    if (result.status === "ok") {
      reload();
    }
    return classify(result);
  };

  return (
    <PartyScreen
      state={partyState}
      photos={photos.state.status === "ok" ? photos.state.data : []}
      meDid={authState.status === "signedIn" ? authState.did : undefined}
      onRetry={reload}
      onLeave={handleLeave}
      onToggleLock={handleToggleLock}
      onRename={handleRename}
    />
  );
}
