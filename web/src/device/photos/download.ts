"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ChainPhotos } from "./index";
import type { Media } from "@/domain/types";

export type DownloadableMedia = Media & {
  ownerDid?: string;
  hidden?: boolean;
  takenAt?: string;
};

const savingMedia = new Set<string>();

export function shouldSavePhoto(media: DownloadableMedia, meDid: string): boolean {
  return media.ownerDid !== meDid && media.hidden !== true && media.url !== "";
}

export async function savePartyPhotos(
  photos: DownloadableMedia[],
  meDid: string,
  album: string,
): Promise<number> {
  let saved = 0;
  for (const photo of photos) {
    if (!shouldSavePhoto(photo, meDid)) {
      continue;
    }
    const key = `chain.party.photo.saved.${photo.id}`;
    let nativeId: string | null = null;
    try {
      nativeId = localStorage.getItem(key);
    } catch {
      nativeId = null;
    }
    if (nativeId) {
      // 記録があれば、アルバムから削除されていても再保存しない。
      continue;
    }
    if (savingMedia.has(photo.id)) {
      continue;
    }
    savingMedia.add(photo.id);
    try {
      const result = await ChainPhotos.savePhoto({
        url: photo.url,
        album,
        takenAt: photo.takenAt ?? new Date().toISOString(),
        fileName: `${photo.id}.jpg`,
      });
      try {
        localStorage.setItem(key, result.mediaStoreId);
      } catch {
        // 保存記録を書けなくても、今回の保存自体は成功として扱う。
      }
      saved += 1;
    } finally {
      savingMedia.delete(photo.id);
    }
  }
  return saved;
}

export function usePartyPhotoDownload(
  photos: DownloadableMedia[],
  enabled: boolean,
  meDid: string,
  album: string,
): { save: () => Promise<number>; saving: boolean } {
  const [saving, setSaving] = useState(false);
  const latestPhotos = useRef(photos);
  useEffect(() => {
    latestPhotos.current = photos;
  }, [photos]);
  const running = useRef<Promise<number> | null>(null);
  const rerun = useRef(false);
  const save = useCallback(async () => {
    if (running.current) {
      rerun.current = true;
      return running.current;
    }
    setSaving(true);
    const task = (async () => {
      let total = 0;
      do {
        rerun.current = false;
        total += await savePartyPhotos(latestPhotos.current, meDid, album);
      } while (rerun.current);
      return total;
    })().finally(() => {
      running.current = null;
      setSaving(false);
    });
    running.current = task;
    return task;
  }, [album, meDid]);
  useEffect(() => {
    if (enabled) {
      queueMicrotask(() => void save());
    }
  }, [enabled, photos, save]);
  return { save, saving };
}
