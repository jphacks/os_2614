import { WebPlugin } from "@capacitor/core";
import type {
  ChainPhotosPlugin,
  PendingPhoto,
  PhotoPermissionStatus,
  PhotoWatchError,
  ReadPhotoResult,
} from "./definitions";

/** ブラウザ用の写真モック。Playwright から pending と保存結果を注入できる。 */
export class ChainPhotosWeb extends WebPlugin implements ChainPhotosPlugin {
  private permission: PhotoPermissionStatus = { photos: "granted" };
  private pending: PendingPhoto[] = [];
  private saved = new Set<string>();
  private seen = new Set<string>();
  private watching = false;
  private since = "";
  private saveCalls = 0;
  private startError: Error | undefined;
  private blobUrls = new Set<string>();

  constructor() {
    super();
    const initialPermission = typeof window !== "undefined"
      ? (window as unknown as { __chainPhotosMockPermission?: PhotoPermissionStatus }).__chainPhotosMockPermission
      : undefined;
    if (initialPermission) {
      this.permission = initialPermission;
    }
    const initialPending = typeof window !== "undefined"
      ? (window as unknown as { __chainPhotosMockPending?: PendingPhoto[] }).__chainPhotosMockPending
      : undefined;
    if (initialPending) {
      // native と同じく、foundAt の無い古い項目は takenAt を foundAt として扱う。
      this.pending = initialPending.map((item) => ({ ...item, foundAt: item.foundAt ?? item.takenAt }));
    }
    if (typeof window !== "undefined") {
      const bridge = ((window as unknown as { __chainPhotosMock?: Record<string, unknown> }).__chainPhotosMock ?? {}) as Record<string, unknown>;
      const calls = Array.isArray(bridge.calls) ? bridge.calls : [];
      bridge.calls = calls;
      bridge.emit = (photo: PendingPhoto) => this.emitPhoto(photo);
      bridge.seed = (photo: PendingPhoto) => this.pending.push({ ...photo, foundAt: photo.foundAt ?? photo.takenAt });
      bridge.emitWatchError = (reason: PhotoWatchError["reason"]) => this.notifyListeners("watchError", { reason });
      bridge.failStart = (message: string | null) => { this.startError = message === null ? undefined : new Error(message); };
      bridge.setPermission = (permission: PhotoPermissionStatus) => this.setPermission(permission);
      bridge.state = () => ({ pending: this.pending.map((item) => ({ ...item })), saved: [...this.saved], saveCalls: this.saveCalls, calls });
      if (typeof bridge.startError === "string") {
        this.startError = new Error(bridge.startError);
      }
      bridge.ready = true;
      (window as unknown as { __chainPhotosMock?: unknown }).__chainPhotosMock = bridge;
    }
  }

  async checkPermissions(): Promise<PhotoPermissionStatus> {
    return { ...this.permission };
  }

  async requestPermissions(): Promise<PhotoPermissionStatus> {
    return { ...this.permission };
  }

  async startWatching(_opts: { since: string }): Promise<void> {
    this.recordCall("startWatching");
    if (this.startError) {
      throw this.startError;
    }
    if (_opts.since !== this.since) {
      this.seen.clear();
      this.since = _opts.since;
    }
    this.watching = true;
  }

  async stopWatching(): Promise<void> {
    this.watching = false;
  }

  async listPending(): Promise<{ items: PendingPhoto[] }> {
    return { items: this.pending.map((item) => ({ ...item })) };
  }

  async removePending(opts: { ids: string[] }): Promise<void> {
    const ids = new Set(opts.ids);
    ids.forEach((id) => this.seen.add(id));
    this.pending = this.pending.filter((item) => !ids.has(item.mediaStoreId));
  }

  /** path には fetch できる blob URL を返す。実機の一時ファイル path に相当する。 */
  async readPhoto(_opts: { mediaStoreId: string }): Promise<ReadPhotoResult> {
    void _opts;
    const blob = new Blob(["hello"], { type: "image/jpeg" });
    const path = URL.createObjectURL(blob);
    this.blobUrls.add(path);
    return { path, mime: "image/jpeg", size: blob.size };
  }

  async releasePhoto(opts: { path: string }): Promise<void> {
    this.recordCall("releasePhoto");
    if (this.blobUrls.delete(opts.path)) {
      URL.revokeObjectURL(opts.path);
    }
  }

  async savePhoto(_opts: {
    url: string;
    album: string;
    takenAt: string;
    fileName: string;
  }): Promise<{ mediaStoreId: string }> {
    void _opts;
    this.saveCalls += 1;
    this.recordCall("savePhoto");
    const mediaStoreId = `web-saved-${Date.now()}`;
    this.saved.add(mediaStoreId);
    return { mediaStoreId };
  }

  async isSaved(opts: { mediaStoreId: string }): Promise<{ saved: boolean }> {
    return { saved: this.saved.has(opts.mediaStoreId) };
  }

  async removeAllListeners(): Promise<void> {
    await super.removeAllListeners();
  }

  /** テスト用。実機では ContentObserver が同じイベントを発火する。 */
  emitPhoto(input: Omit<PendingPhoto, "foundAt"> & { foundAt?: string }): void {
    const photo: PendingPhoto = { ...input, foundAt: input.foundAt ?? new Date().toISOString() };
    if (!this.watching || this.seen.has(photo.mediaStoreId) || !photo.uri.includes("DCIM/Camera")) {
      return;
    }
    this.pending.push(photo);
    this.notifyListeners("photoCaptured", photo);
  }

  setPermission(permission: PhotoPermissionStatus): void {
    this.permission = permission;
  }

  private recordCall(name: string): void {
    if (typeof window === "undefined") return;
    const bridge = (window as unknown as { __chainPhotosMock?: { calls?: string[] } }).__chainPhotosMock;
    bridge?.calls?.push(name);
    try {
      if (bridge?.calls) sessionStorage.setItem("chain.photo.test.calls", JSON.stringify(bridge.calls));
    } catch {
      // テスト用の記録が使えない環境でもモック本体は動かす。
    }
  }
}
