"use client";

import { useEffect, useSyncExternalStore } from "react";
import type { Party } from "@/domain/types";
import { DEFAULT_PHOTO_SETTINGS, type PhotoSettings } from "./settings";
import { photoUploadController, type PartyPhotoWindow } from "./uploadQueue";

export {
  PHOTO_EXPIRY_MS,
  PHOTO_RETRY_MAX_MS,
  PHOTO_RETRY_MIN_MS,
  PHOTO_WAIT_MS,
  isPhotoInParty,
  retryDelay,
  selectPartyForPhoto,
  type PartyPhotoWindow,
} from "./uploadQueue";

export type AutoShareState = {
  pendingCount: number;
  cancel: () => Promise<void>;
};

/**
 * 前面の party 画面から起動する自動共有パイプライン。
 * キューとタイマーは uploadQueue のコントローラが持ち、ここは入力を渡して状態を購読するだけ。
 */
export function useAutoShare(
  party: Party | null,
  settings: PhotoSettings = DEFAULT_PHOTO_SETTINGS,
  candidates: PartyPhotoWindow[] = [],
): AutoShareState {
  const pendingCount = useSyncExternalStore(
    photoUploadController.subscribe,
    photoUploadController.getPendingCount,
    () => 0,
  );
  const partyId = party?.phase === "confirmed" ? party.id : undefined;
  const confirmedAt = party?.phase === "confirmed" ? party.confirmedAt : undefined;
  const candidatesKey = candidates
    .map((candidate) => `${candidate.id}|${candidate.confirmedAt ?? ""}|${candidate.endedAt ?? ""}`)
    .join(",");
  useEffect(() => {
    photoUploadController.update({
      autoShare: settings.autoShare,
      party: partyId ? { id: partyId, confirmedAt } : null,
      candidates,
    });
    // candidates は中身が変わった時 (candidatesKey) だけ反映する。毎回の再生成では監視をやり直さない。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settings.autoShare, partyId, confirmedAt, candidatesKey]);
  return { pendingCount, cancel: photoUploadController.cancel };
}
