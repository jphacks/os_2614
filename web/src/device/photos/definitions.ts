import type { PermissionState, PluginListenerHandle } from "@capacitor/core";

export type PhotoPermission = PermissionState | "limited";

export type PhotoPermissionStatus = {
  photos: PhotoPermission;
};

export type PendingPhoto = {
  mediaStoreId: string;
  takenAt: string;
  uri: string;
  /** native が最初にキューへ入れた時刻 (ISO)。永続化され、アップロード期限の基準になる。 */
  foundAt: string;
};

/** readPhoto が書き出した一時ファイル。使い終わったら releasePhoto で消す。 */
export type ReadPhotoResult = {
  path: string;
  mime: "image/jpeg" | "image/png" | "image/webp";
  size: number;
};

export type PhotoWatchError = {
  reason: "permission_revoked" | "query_failed";
};

export type ChainPhotosPlugin = {
  checkPermissions(): Promise<PhotoPermissionStatus>;
  requestPermissions(): Promise<PhotoPermissionStatus>;
  startWatching(opts: { since: string }): Promise<void>;
  stopWatching(): Promise<void>;
  listPending(): Promise<{ items: PendingPhoto[] }>;
  removePending(opts: { ids: string[] }): Promise<void>;
  readPhoto(opts: { mediaStoreId: string }): Promise<ReadPhotoResult>;
  releasePhoto(opts: { path: string }): Promise<void>;
  savePhoto(opts: {
    url: string;
    album: string;
    takenAt: string;
    fileName: string;
  }): Promise<{ mediaStoreId: string }>;
  isSaved(opts: { mediaStoreId: string }): Promise<{ saved: boolean }>;
  addListener(
    eventName: "photoCaptured",
    listener: (photo: PendingPhoto) => void,
  ): Promise<PluginListenerHandle>;
  addListener(
    eventName: "watchError",
    listener: (error: PhotoWatchError) => void,
  ): Promise<PluginListenerHandle>;
  removeAllListeners(): Promise<void>;
};
