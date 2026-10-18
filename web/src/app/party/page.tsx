"use client";

// /party は static export なので SSR ではデータを取らない。サインイン後に useResource が
// GET /party/current を叩き、DTO を toParty でドメインへ写す。null は「所属パーティ無し」。
// `party.updated` (10 §7.3) はキャッシュに溜まり、ここで取得結果へ重ねて反映する。

import { useAuth } from "@/auth";
import { useCallback, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  getJson,
  patchJson,
  postJson,
  toResult,
  type ResourceResult,
} from "@/data/api";
import type { InstancePost, Party as PartyDTO } from "@/data/dto";
import { usePagedResource, useResource, type ResourceState } from "@/data/hooks";
import { toParty, toPost } from "@/data/mappers";
import type { Media, Party } from "@/domain/types";
import type { PartyUpdatedData } from "@/realtime/events";
import {
  useRealtimeEvent,
  useRealtimeResync,
  useRealtimeSubscription,
} from "@/realtime/provider";
import { useRealtimeCache } from "@/realtime/store";
import { PartyScreen, type PartyActionResult } from "@/components/screens/PartyScreen";
import { usePhotoSettings } from "@/device/photos/settings";
import { usePartyPhotoDownload, type DownloadableMedia } from "@/device/photos/download";

function classify(result: ResourceResult<unknown>): PartyActionResult {
  return result.status === "ok" ? { status: "ok" } : { status: result.status };
}

/** リアルタイムの party.updated を、取得済みの party へ重ねる (名前 / phase / 固定 / 在席)。 */
function withRealtimeUpdate(party: Party, update: PartyUpdatedData): Party {
  const present = new Set(update.present);
  return {
    ...party,
    name: update.name,
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
  const router = useRouter();
  const [photoSettings] = usePhotoSettings();
  const [endedNotice, setEndedNotice] = useState(false);
  const [endedPartyId, setEndedPartyId] = useState("");
  const handledUpdate = useRef<string | null>(null);
  const { state, reload } = useResource<PartyDTO | null>(
    (signal) => getJson<PartyDTO | null>("/party/current", { signal }),
    [],
    { cacheKey: "/party/current" },
  );
  const cache = useRealtimeCache();
  const realtime = cache.party;

  const partyId = state.status === "ok" && state.data ? state.data.id : "";
  const endedPosts = usePagedResource<InstancePost>(
    endedPartyId ? `/instances/${endedPartyId}/posts?roots=1` : "",
    [endedPartyId],
  );
  const {
    state: postsState,
    items,
    hasMore,
    loadingMore,
    loadMore,
    reload: reloadPosts,
  } = usePagedResource<InstancePost>(partyId ? `/instances/${partyId}/posts?roots=1` : "", [partyId]);
  const [replyDelta, setReplyDelta] = useState<Record<string, number>>({});

  const posts = useMemo(
    () => items
      .filter((item) => !item.hidden && item.moderation?.display !== "hide")
      .map((item) => {
        const post = toPost(item);
        const delta = replyDelta[post.id] ?? 0;
        return delta === 0 ? post : { ...post, replyCount: post.replyCount + delta };
      }),
    [items, replyDelta],
  );
  const photos: Media[] = posts.flatMap((post) => post.attachments);
  const endedPhotos = endedPosts.items.flatMap((item) => toPost(item).attachments);
  const visiblePhotos = partyId ? photos : endedPhotos;
  const downloadablePhotos: DownloadableMedia[] = useMemo(
    () => posts.flatMap((post) =>
      post.attachments.map((media) => ({
        ...media,
        ownerDid: post.author.did,
        hidden: post.moderation?.display === "hide",
        takenAt: post.takenAt,
      })),
    ),
    [posts],
  );
  const endedDownloadablePhotos: DownloadableMedia[] = useMemo(
    () => endedPosts.items.flatMap((item) => {
      const post = toPost(item);
      return post.attachments.map((media) => ({
        ...media,
        ownerDid: post.author.did,
        hidden: post.moderation?.display === "hide",
        takenAt: post.takenAt,
      }));
    }),
    [endedPosts.items],
  );
  useRealtimeSubscription(partyId || null);
  const reloadTimeline = useCallback(() => {
    setReplyDelta({});
    reloadPosts();
  }, [reloadPosts]);
  useRealtimeResync(() => {
    reload();
    reloadTimeline();
  });
  useRealtimeEvent("post.created", (post) => {
    if (post.spaceId === partyId) {
      if (post.reply?.root) {
        setReplyDelta((current) => ({
          ...current,
          [post.reply!.root]: (current[post.reply!.root] ?? 0) + 1,
        }));
      } else {
        reloadTimeline();
      }
    }
  });
  useRealtimeEvent("post.deleted", (_data, frame) => {
    if (frame.space === partyId) {
      reloadTimeline();
    }
  });
  useRealtimeEvent("media.created", (_media, frame) => {
    if (frame.space === partyId) {
      reloadTimeline();
    }
  });
  useRealtimeEvent("party.updated", (update) => {
    const key = `${update.id}:${update.name}:${update.phase}:${update.locked}:${update.present.join(",")}`;
    if (handledUpdate.current === key) {
      return;
    }
    handledUpdate.current = key;
    const currentId = state.status === "ok" && state.data ? state.data.id : "";
    if (update.phase === "ended") {
      setEndedNotice(true);
      setEndedPartyId(update.id);
    }
    if (!currentId || update.id !== currentId || update.phase === "ended" || update.phase === "dissolved") {
      reload();
    }
  });

  // ok のときだけ DTO をドメイン Party に写し、リアルタイム更新を重ねる。
  // null は empty、それ以外は状態をそのまま渡す。
  const partyState: ResourceState<Party> =
    state.status === "ok"
      ? state.data === null
        ? { status: "empty" }
        : {
            status: "ok",
            data:
              realtime &&
              realtime.id === state.data.id &&
              realtime.phase !== "ended" &&
              realtime.phase !== "dissolved"
                ? withRealtimeUpdate(toParty(state.data), realtime)
                : toParty(state.data),
          }
      : state;

  const party = partyState.status === "ok" ? partyState.data : null;
  const photoDownload = usePartyPhotoDownload(
    partyId ? downloadablePhotos : endedDownloadablePhotos,
    photoSettings.saveToDevice && (party !== null || endedNotice),
    authState.status === "signedIn" ? authState.did : "",
    party?.name || "Party",
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
    // lock/unlock は 08 §4.4 の API を呼び、成功したときだけ画面の状態を更新する。
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
      photos={visiblePhotos}
      posts={posts}
      postsState={postsState.status === "ok" ? { status: "ok", data: posts } : postsState}
      hasMore={hasMore}
      loadingMore={loadingMore}
      onLoadMore={loadMore}
      meDid={authState.status === "signedIn" ? authState.did : undefined}
      onRetry={reload}
      onLeave={handleLeave}
      onToggleLock={handleToggleLock}
      onRename={handleRename}
      onReply={(post) => {
        if (party) {
          router.push(`/party/replies/?id=${encodeURIComponent(party.id)}&post=${encodeURIComponent(post.id)}`);
        }
      }}
      endedNotice={endedNotice}
      onSavePhotos={() => void photoDownload.save()}
      savingPhotos={photoDownload.saving}
    />
  );
}
