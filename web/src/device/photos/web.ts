import { WebPlugin } from "@capacitor/core";
import type {
  ChainPhotosPlugin,
  PendingPhoto,
  PhotoPermissionStatus,
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
      this.pending = initialPending.map((item) => ({ ...item }));
    }
    if (typeof window !== "undefined") {
      const bridge = ((window as unknown as { __chainPhotosMock?: Record<string, unknown> }).__chainPhotosMock ?? {}) as Record<string, unknown>;
      const calls = Array.isArray(bridge.calls) ? bridge.calls : [];
      bridge.calls = calls;
      bridge.emit = (photo: PendingPhoto) => this.emitPhoto(photo);
      bridge.seed = (photo: PendingPhoto) => this.pending.push(photo);
      bridge.setPermission = (permission: PhotoPermissionStatus) => this.setPermission(permission);
      bridge.state = () => ({ pending: this.pending.map((item) => ({ ...item })), saved: [...this.saved], saveCalls: this.saveCalls, calls });
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

  async readPhoto(_opts: { mediaStoreId: string }): Promise<ReadPhotoResult> {
    void _opts;
    return { data: "aGVsbG8=", mime: "image/jpeg" };
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
  emitPhoto(photo: PendingPhoto): void {
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
