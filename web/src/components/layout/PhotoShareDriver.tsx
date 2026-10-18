"use client";

import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import type { Party as PartyDTO, Memory as MemoryDTO } from "@/data/dto";
import { getJson } from "@/data/api";
import { usePagedResource, useResource } from "@/data/hooks";
import { toMemory, toParty } from "@/data/mappers";
import { useAuth } from "@/auth";
import { usePhotoSettings, readPhotoSettings, setPhotoPermissionNotice, writePhotoSettings } from "@/device/photos/settings";
import { photoUploadController } from "@/device/photos/uploadQueue";
import { ChainPhotos } from "@/device/photos";
import { useAutoShare, type AutoShareState, type PartyPhotoWindow } from "@/device/photos/autoshare";
import { useRealtimeEvent } from "@/realtime/provider";

type PhotoShareSnapshot = AutoShareState & { autoShareEnabled: boolean };
const EMPTY: PhotoShareSnapshot = {
  pendingCount: 0,
  cancel: async () => undefined,
  autoShareEnabled: false,
};
let snapshot = EMPTY;
const listeners = new Set<() => void>();

/** API が 404 や空 JSON を返しても常駐ドライバを壊さない。 */
export function memoryItemsOrEmpty(value: unknown): MemoryDTO[] {
  if (!value || typeof value !== "object") return [];
  const items = (value as { items?: unknown }).items;
  return Array.isArray(items) ? (items as MemoryDTO[]) : [];
}

function setPhotoShareSnapshot(next: PhotoShareSnapshot): void {
  snapshot = next;
  listeners.forEach((listener) => listener());
}

export function usePhotoShare(): PhotoShareSnapshot {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    () => snapshot,
    () => EMPTY,
  );
}

/** layout の children を包まず、サインイン中の自動共有だけを常駐させる。 */
export function PhotoShareDriver() {
  const { state: auth } = useAuth();
  const [settings] = usePhotoSettings();
  const wasOn = useRef(false);
  useEffect(() => {
    if (auth.status !== "signedIn" || !settings.autoShare) {
      setPhotoShareSnapshot(EMPTY);
      // オン→オフに変わった時だけ止めて破棄する。起動時からオフなら ChainPhotos には触れない。
      if (wasOn.current) {
        wasOn.current = false;
        // 設定の読み込み前 (state は既定値) では捨てない。保存値がオフの時だけ破棄する。
        photoUploadController.stop(!readPhotoSettings().autoShare);
      }
      return;
    }
    wasOn.current = true;
    let active = true;
    void ChainPhotos.checkPermissions().then((permission) => {
      if (active && permission.photos !== "granted") {
        writePhotoSettings({ ...settings, autoShare: false });
        setPhotoPermissionNotice();
      }
    });
    return () => { active = false; };
  }, [auth.status, settings]);
  if (auth.status !== "signedIn" || !settings.autoShare) {
    return null;
  }
  return <PhotoShareDriverActive settings={settings} />;
}

function PhotoShareDriverActive({ settings }: { settings: { autoShare: boolean; saveToDevice: boolean } }) {
  const current = useResource<PartyDTO | null>(
    (signal) => getJson<PartyDTO | null>("/party/current", { signal }),
    [],
    { cacheKey: "/party/current" },
  );
  useRealtimeEvent("party.updated", () => current.reload());

  if (current.state.status !== "ok") {
    return null;
  }
  const party = current.state.data ? toParty(current.state.data) : null;
  return party ? (
    <PhotoShareRuntime party={party} settings={settings} candidates={[]} />
  ) : (
    <ArchivedPhotoShare settings={settings} />
  );
}

function ArchivedPhotoShare({ settings }: { settings: { autoShare: boolean; saveToDevice: boolean } }) {
  const [pending, setPending] = usePendingPhotos();
  const memories = usePagedResource<MemoryDTO>(
    pending !== null && pending.length > 0 ? "/memories" : "",
    [pending?.length ?? -1],
  );
  if (pending === null || (pending.length > 0 && memories.state.status === "loading")) {
    return null;
  }
  const memoryItems = memoryItemsOrEmpty({ items: memories.items });
  const candidates: PartyPhotoWindow[] = memoryItems
    .map(toMemory)
    .filter((memory) => memory.type === "party")
    .map((memory) => ({
      id: memory.spaceId,
      confirmedAt: memory.confirmedAt ?? memory.from,
      endedAt: memory.endedAt ?? memory.until,
    }));
  return <PhotoShareRuntime party={null} settings={settings} candidates={candidates} onPending={setPending} />;
}

function usePendingPhotos(): [string[] | null, (items: string[]) => void] {
  const [items, setItems] = useState<string[] | null>(null);
  useEffect(() => {
    void ChainPhotos.listPending()
      .then((result) => setItems(result.items.map((item) => item.mediaStoreId)))
      .catch(() => setItems([]));
  }, []);
  return [items, setItems];
}

function PhotoShareRuntime({
  party,
  settings,
  candidates,
  onPending,
}: {
  party: ReturnType<typeof toParty> | null;
  settings: { autoShare: boolean; saveToDevice: boolean };
  candidates: PartyPhotoWindow[];
  onPending?: (items: string[]) => void;
}) {
  const share = useAutoShare(party, settings, candidates);
  useEffect(() => {
    setPhotoShareSnapshot({ ...share, autoShareEnabled: settings.autoShare });
    return () => setPhotoShareSnapshot(EMPTY);
  }, [share, settings.autoShare]);
  useEffect(() => {
    if (onPending) {
      void ChainPhotos.listPending().then((result) => onPending(result.items.map((item) => item.mediaStoreId)));
    }
  }, [onPending, share.pendingCount]);
  return null;
}
