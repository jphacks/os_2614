import type { PermissionState, PluginListenerHandle } from "@capacitor/core";

export type PhotoPermission = PermissionState | "limited";

export type PhotoPermissionStatus = {
  photos: PhotoPermission;
};

export type PendingPhoto = {
  mediaStoreId: string;
  takenAt: string;
  uri: string;
};

export type ReadPhotoResult = {
  data: string;
  mime: string;
};

export type ChainPhotosPlugin = {
  checkPermissions(): Promise<PhotoPermissionStatus>;
  requestPermissions(): Promise<PhotoPermissionStatus>;
  startWatching(opts: { since: string }): Promise<void>;
  stopWatching(): Promise<void>;
  listPending(): Promise<{ items: PendingPhoto[] }>;
  removePending(opts: { ids: string[] }): Promise<void>;
  readPhoto(opts: { mediaStoreId: string }): Promise<ReadPhotoResult>;
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
  removeAllListeners(): Promise<void>;
};
