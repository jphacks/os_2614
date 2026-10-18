import { Capacitor } from "@capacitor/core";
import { postForm, postJson } from "@/data/api";
import { ChainPhotos, type PendingPhoto } from "./index";
import { readPhotoSettings, setPhotoPermissionNotice, writePhotoSettings } from "./settings";

export const PHOTO_WAIT_MS = 60_000;
export const PHOTO_RETRY_MIN_MS = 30_000;
export const PHOTO_RETRY_MAX_MS = 10 * 60_000;
export const PHOTO_EXPIRY_MS = 24 * 60 * 60_000;

export type PartyPhotoWindow = {
  id: string;
  confirmedAt?: string;
  endedAt?: string;
};

/** コントローラへ渡す、React から切り離した入力。 */
export type UploadQueueInput = {
  autoShare: boolean;
  party: { id: string; confirmedAt?: string } | null;
  candidates: PartyPhotoWindow[];
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

function sentKey(partyId: string, mediaStoreId: string): string {
  return `chain.party.photo.sent.${partyId}.${mediaStoreId}`;
}

/** 実機は一時ファイルを WebView 経由で読む。ブラウザのモックは fetch できる URL をそのまま返す。 */
function fileUrl(path: string): string {
  return Capacitor.isNativePlatform() ? Capacitor.convertFileSrc(`file://${path}`) : path;
}

function extensionOf(mime: string): string {
  if (mime === "image/png") return "png";
  if (mime === "image/webp") return "webp";
  return "jpg";
}

async function uploadPending(photo: PendingPhoto, party: PartyPhotoWindow): Promise<void> {
  const sent = localStorage.getItem(sentKey(party.id, photo.mediaStoreId));
  if (sent) {
    await ChainPhotos.removePending({ ids: [photo.mediaStoreId] });
    return;
  }
  const read = await ChainPhotos.readPhoto({ mediaStoreId: photo.mediaStoreId });
  let mediaId: string;
  try {
    const response = await fetch(fileUrl(read.path));
    if (!response.ok) {
      throw new Error(`photo file read failed: ${response.status}`);
    }
    const blob = await response.blob();
    const form = new FormData();
    form.append("file", new File([blob], `${photo.mediaStoreId}.${extensionOf(read.mime)}`, { type: read.mime }));
    form.append("takenAt", photo.takenAt);
    mediaId = (await postForm<{ id: string }>("/media", form)).id;
  } finally {
    // 成否にかかわらず一時ファイルは必ず消す。
    await ChainPhotos.releasePhoto({ path: read.path }).catch(() => undefined);
  }
  await postJson(`/instances/${encodeURIComponent(party.id)}/media`, {
    mediaId,
    takenAt: photo.takenAt,
  });
  localStorage.setItem(sentKey(party.id, photo.mediaStoreId), String(Date.now()));
  await ChainPhotos.removePending({ ids: [photo.mediaStoreId] });
}

type Entry = {
  photo: PendingPhoto;
  target: PartyPhotoWindow;
  dueAt: number;
  attempt: number;
  timer?: ReturnType<typeof setTimeout>;
};

type Handle = { remove: () => Promise<void> };

/**
 * 自動共有のキュー・タイマー・アップロードを React の外で持つコントローラ。
 * 期限は native の foundAt + 60 秒なので、アプリを開き直しても待ち時間を延ばさない。
 */
export class PhotoUploadController {
  private input: UploadQueueInput = { autoShare: false, party: null, candidates: [] };
  private watchKey = "";
  private candidatesKey = "";
  private generation = 0;
  private active = false;
  private needsRetry = false;
  private entries = new Map<string, Entry>();
  private ready: string[] = [];
  private inFlight = new Set<string>();
  private pumping = false;
  private chain: Promise<void> = Promise.resolve();
  private listeners = new Set<() => void>();
  private handles: Promise<Handle>[] = [];

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };

  getPendingCount = (): number => this.entries.size;

  /** 現在の party / 候補 / 設定を渡す。監視の開始・再開は primitive な値が変わった時だけ行う。 */
  update(input: UploadQueueInput): void {
    if (!input.autoShare) {
      this.stop(false);
      return;
    }
    const party = input.party;
    const watchKey = `${party?.id ?? ""}|${party?.confirmedAt ?? ""}`;
    const candidatesKey = input.candidates
      .map((candidate) => `${candidate.id}|${candidate.confirmedAt ?? ""}|${candidate.endedAt ?? ""}`)
      .join(",");
    this.input = input;
    const wasActive = this.active;
    if (!wasActive) {
      this.attach();
    }
    if (!wasActive || watchKey !== this.watchKey) {
      this.watchKey = watchKey;
      this.candidatesKey = candidatesKey;
      this.enqueueSync(true);
    } else if (candidatesKey !== this.candidatesKey) {
      this.candidatesKey = candidatesKey;
      this.enqueueSync(false);
    }
  }

  /** 監視とタイマーを止める。discard が true なら native の pending も捨てる。 */
  stop(discard: boolean): void {
    const wasActive = this.active;
    this.generation += 1;
    this.active = false;
    this.watchKey = "";
    this.candidatesKey = "";
    this.needsRetry = false;
    this.clearQueue();
    this.detach();
    if (wasActive) {
      void ChainPhotos.stopWatching().catch(() => undefined);
    }
    if (discard) {
      void this.removeAllPending();
    }
  }

  /** 待機中の写真を取り消し、再スキャンでも戻らないよう native 側で既読にする。 */
  cancel = async (): Promise<void> => {
    this.clearQueue();
    const pending = await ChainPhotos.listPending();
    const ids = pending.items.map((item) => item.mediaStoreId);
    if (ids.length > 0) {
      await ChainPhotos.removePending({ ids });
    }
  };

  private clearQueue(): void {
    for (const entry of this.entries.values()) {
      clearTimeout(entry.timer);
    }
    this.entries.clear();
    this.ready = [];
    this.emit();
  }

  private emit(): void {
    this.listeners.forEach((listener) => listener());
  }

  private async removeAllPending(): Promise<void> {
    try {
      const pending = await ChainPhotos.listPending();
      if (pending.items.length > 0) {
        await ChainPhotos.removePending({ ids: pending.items.map((item) => item.mediaStoreId) });
      }
    } catch {
      // 破棄に失敗しても次回の停止時にやり直せる。
    }
  }

  private attach(): void {
    this.active = true;
    pruneSentRecords();
    this.handles = [
      ChainPhotos.addListener("photoCaptured", (photo) => {
        this.process(photo, this.generation);
      }),
      ChainPhotos.addListener("watchError", (error) => {
        if (error.reason === "permission_revoked") {
          this.revokePermission();
        }
        // query_failed は native が監視を続けるので何もしない。
      }),
    ];
    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", this.onVisible);
    }
  }

  private detach(): void {
    const handles = this.handles;
    this.handles = [];
    for (const handle of handles) {
      void handle.then((h) => h.remove()).catch(() => undefined);
    }
    if (typeof document !== "undefined") {
      document.removeEventListener("visibilitychange", this.onVisible);
    }
  }

  private onVisible = (): void => {
    if (document.visibilityState === "visible" && this.needsRetry && this.active) {
      this.needsRetry = false;
      this.enqueueSync(true);
    }
  };

  /** 権限が失われた時は設定をオフにして、次回の Notice を立てる。 */
  private revokePermission(): void {
    writePhotoSettings({ ...readPhotoSettings(), autoShare: false });
    setPhotoPermissionNotice();
  }

  private enqueueSync(restartWatching: boolean): void {
    const generation = this.generation;
    this.chain = this.chain
      .then(() => this.sync(generation, restartWatching))
      .catch(() => undefined);
  }

  private async sync(generation: number, restartWatching: boolean): Promise<void> {
    if (generation !== this.generation) return;
    const { party } = this.input;
    if (restartWatching) {
      const permission = await ChainPhotos.checkPermissions();
      if (generation !== this.generation) return;
      if (permission.photos !== "granted") {
        this.revokePermission();
        return;
      }
      if (party?.confirmedAt) {
        try {
          await ChainPhotos.startWatching({ since: party.confirmedAt });
        } catch {
          if (generation !== this.generation) return;
          const after = await ChainPhotos.checkPermissions().catch(() => null);
          if (after && after.photos !== "granted") {
            this.revokePermission();
            return;
          }
          // 権限以外の失敗は設定を保ち、次に前面へ戻った時に再試行する。
          this.needsRetry = true;
        }
      } else {
        await ChainPhotos.stopWatching();
      }
    }
    const pending = await ChainPhotos.listPending();
    if (generation !== this.generation) return;
    pending.items.forEach((photo) => this.process(photo, generation));
  }

  private process(photo: PendingPhoto, generation: number): void {
    if (generation !== this.generation || !this.active) return;
    const { party, candidates } = this.input;
    const target = selectPartyForPhoto(photo, [
      ...(party?.confirmedAt ? [{ id: party.id, confirmedAt: party.confirmedAt }] : []),
      ...candidates,
    ]);
    if (!target || (target.id !== party?.id && Date.now() - Date.parse(target.endedAt ?? "") > PHOTO_EXPIRY_MS)) {
      void ChainPhotos.removePending({ ids: [photo.mediaStoreId] });
      return;
    }
    if (this.entries.has(photo.mediaStoreId)) return;
    const found = Date.parse(photo.foundAt);
    const dueAt = (Number.isFinite(found) ? found : Date.now()) + PHOTO_WAIT_MS;
    const entry: Entry = { photo, target, dueAt, attempt: 0 };
    this.entries.set(photo.mediaStoreId, entry);
    this.schedule(entry);
    this.emit();
  }

  private schedule(entry: Entry): void {
    clearTimeout(entry.timer);
    const id = entry.photo.mediaStoreId;
    entry.timer = setTimeout(() => {
      entry.timer = undefined;
      this.ready.push(id);
      void this.pump();
    }, Math.max(0, entry.dueAt - Date.now()));
  }

  /** 期限の来た写真を 1 枚ずつ送る。同じ写真を同時に二重送信しない。 */
  private async pump(): Promise<void> {
    if (this.pumping) return;
    this.pumping = true;
    try {
      for (let id = this.ready.shift(); id !== undefined; id = this.ready.shift()) {
        const entry = this.entries.get(id);
        if (!entry || this.inFlight.has(id)) continue;
        await this.upload(entry);
      }
    } finally {
      this.pumping = false;
    }
  }

  private async upload(entry: Entry): Promise<void> {
    const id = entry.photo.mediaStoreId;
    if (Date.now() - Date.parse(entry.photo.takenAt) > PHOTO_EXPIRY_MS) {
      this.entries.delete(id);
      this.emit();
      void ChainPhotos.removePending({ ids: [id] });
      return;
    }
    this.inFlight.add(id);
    try {
      await uploadPending(entry.photo, entry.target);
      if (this.entries.get(id) === entry) {
        this.entries.delete(id);
        this.emit();
      }
    } catch {
      if (this.entries.get(id) === entry) {
        entry.dueAt = Date.now() + retryDelay(entry.attempt);
        entry.attempt += 1;
        this.schedule(entry);
      }
    } finally {
      this.inFlight.delete(id);
    }
  }
}

export const photoUploadController = new PhotoUploadController();
