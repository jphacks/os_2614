"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { Party } from "@/domain/types";
import { postForm, postJson } from "@/data/api";
import { ChainPhotos, type PendingPhoto } from "./index";
import { DEFAULT_PHOTO_SETTINGS, type PhotoSettings, setPhotoPermissionNotice, writePhotoSettings } from "./settings";

export const PHOTO_WAIT_MS = 60_000;
export const PHOTO_RETRY_MIN_MS = 30_000;
export const PHOTO_RETRY_MAX_MS = 10 * 60_000;
export const PHOTO_EXPIRY_MS = 24 * 60 * 60_000;

export type PartyPhotoWindow = {
  id: string;
  confirmedAt?: string;
  endedAt?: string;
};

function pruneSentRecords(now = Date.now()): void {
  try {
    for (let i = 0; i < localStorage.length; i += 1) {
      const key = localStorage.key(i);
      if (!key?.startsWith("chain.party.photo.sent.")) continue;
      const value = Number(localStorage.getItem(key));
      if (!Number.isFinite(value) || now - value > 48 * 60 * 60_000) {
        localStorage.removeItem(key);
      }
    }
  } catch {
    // localStorage が使えない場合も写真共有は継続する。
  }
}

/** 写真の撮影時刻が、その party の共有対象期間に入るかを判定する純粋関数。 */
export function isPhotoInParty(photo: PendingPhoto, party: PartyPhotoWindow, now = Date.now()): boolean {
  const taken = Date.parse(photo.takenAt);
  const confirmed = party.confirmedAt ? Date.parse(party.confirmedAt) : Number.NEGATIVE_INFINITY;
  const ended = party.endedAt ? Date.parse(party.endedAt) : now;
  return Number.isFinite(taken) && taken >= confirmed && taken <= ended;
}

/** 現在の party と archived party の候補から、撮影先を決める純粋関数。 */
export function selectPartyForPhoto(
  photo: PendingPhoto,
  parties: PartyPhotoWindow[],
  now = Date.now(),
): PartyPhotoWindow | undefined {
  return parties.find((party) => isPhotoInParty(photo, party, now));
}

export function retryDelay(attempt: number): number {
  return Math.min(PHOTO_RETRY_MAX_MS, PHOTO_RETRY_MIN_MS * 2 ** Math.max(0, attempt));
}

function toFile(data: string, mime: string, name: string): File {
  const raw = data.includes(",") ? data.slice(data.indexOf(",") + 1) : data;
  const bytes = Uint8Array.from(atob(raw), (char) => char.charCodeAt(0));
  return new File([bytes], name, { type: mime || "image/jpeg" });
}

function sentKey(partyId: string, mediaStoreId: string): string {
  return `chain.party.photo.sent.${partyId}.${mediaStoreId}`;
}

async function uploadPending(photo: PendingPhoto, party: PartyPhotoWindow): Promise<void> {
  const sent = localStorage.getItem(sentKey(party.id, photo.mediaStoreId));
  if (sent) {
    await ChainPhotos.removePending({ ids: [photo.mediaStoreId] });
    return;
  }
  const read = await ChainPhotos.readPhoto({ mediaStoreId: photo.mediaStoreId });
  const form = new FormData();
  form.append("file", toFile(read.data, read.mime, `${photo.mediaStoreId}.jpg`));
  form.append("takenAt", photo.takenAt);
  const media = await postForm<{ id: string }>("/media", form);
  await postJson(`/instances/${encodeURIComponent(party.id)}/media`, {
    mediaId: media.id,
    takenAt: photo.takenAt,
  });
  localStorage.setItem(sentKey(party.id, photo.mediaStoreId), String(Date.now()));
  await ChainPhotos.removePending({ ids: [photo.mediaStoreId] });
}

export type AutoShareState = {
  pendingCount: number;
  cancel: () => Promise<void>;
};

/** 前面の party 画面から起動する自動共有パイプライン。 */
export function useAutoShare(
  party: Party | null,
  settings: PhotoSettings = DEFAULT_PHOTO_SETTINGS,
  candidates: PartyPhotoWindow[] = [],
): AutoShareState {
  const [pendingCount, setPendingCount] = useState(0);
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>());
  const queue = useRef<PendingPhoto[]>([]);

  const cancel = useCallback(async () => {
    for (const timer of timers.current.values()) {
      clearTimeout(timer);
    }
    timers.current.clear();
    const ids = queue.current.map((photo) => photo.mediaStoreId);
    queue.current = [];
    setPendingCount(0);
    const pending = await ChainPhotos.listPending();
    const allIds = [...new Set([...ids, ...pending.items.map((item) => item.mediaStoreId)])];
    if (allIds.length > 0) {
      await ChainPhotos.removePending({ ids: allIds });
    }
  }, []);

  useEffect(() => {
    let active = true;
    const timerMap = timers.current;
    pruneSentRecords();
    const process = (photo: PendingPhoto) => {
      if (!active || !settings.autoShare) {
        return;
      }
      const target = selectPartyForPhoto(photo, [
        ...(party?.phase === "confirmed"
          ? [{ id: party.id, confirmedAt: party.confirmedAt }]
          : []),
        ...candidates,
      ]);
      const currentId = party?.id;
      if (!target || (target.id !== currentId && Date.now() - Date.parse(target.endedAt ?? "") > PHOTO_EXPIRY_MS)) {
        void ChainPhotos.removePending({ ids: [photo.mediaStoreId] });
        return;
      }
      if (queue.current.some((item) => item.mediaStoreId === photo.mediaStoreId)) {
        return;
      }
      queue.current.push(photo);
      setPendingCount(queue.current.length);
      const upload = (attempt: number) => {
        if (!active) {
          return;
        }
        const age = Date.now() - Date.parse(photo.takenAt);
        if (age > PHOTO_EXPIRY_MS) {
          void ChainPhotos.removePending({ ids: [photo.mediaStoreId] });
          queue.current = queue.current.filter((item) => item.mediaStoreId !== photo.mediaStoreId);
          setPendingCount(queue.current.length);
          timerMap.delete(photo.mediaStoreId);
          return;
        }
        void uploadPending(photo, target)
          .then(() => {
            queue.current = queue.current.filter((item) => item.mediaStoreId !== photo.mediaStoreId);
            setPendingCount(queue.current.length);
          })
          .catch(() => {
            const retry = setTimeout(() => upload(attempt + 1), retryDelay(attempt));
            timerMap.set(photo.mediaStoreId, retry);
          });
      };
      const timer = setTimeout(() => upload(0), PHOTO_WAIT_MS);
      timerMap.set(photo.mediaStoreId, timer);
    };
    const start = async () => {
      if (!settings.autoShare) {
        await ChainPhotos.stopWatching();
        const pending = await ChainPhotos.listPending();
        if (pending.items.length > 0) {
          await ChainPhotos.removePending({ ids: pending.items.map((item) => item.mediaStoreId) });
        }
        return;
      }
      const permission = await ChainPhotos.checkPermissions();
      if (permission.photos !== "granted") {
        writePhotoSettings({ ...settings, autoShare: false });
        setPhotoPermissionNotice();
        return;
      }
      if (party?.phase === "confirmed" && party.confirmedAt) {
        await ChainPhotos.startWatching({ since: party.confirmedAt });
      } else {
        await ChainPhotos.stopWatching();
      }
      const pending = await ChainPhotos.listPending();
      if (active) {
        pending.items.forEach(process);
      }
    };
    void start();
    const listener = ChainPhotos.addListener("photoCaptured", process);
    return () => {
      active = false;
      void listener.then((handle) => handle.remove());
      void ChainPhotos.stopWatching();
      for (const timer of timerMap.values()) {
        clearTimeout(timer);
      }
      timerMap.clear();
    };
  }, [party, settings, candidates]);

  return { pendingCount, cancel };
}
