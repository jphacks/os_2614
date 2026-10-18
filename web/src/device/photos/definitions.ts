export type ChainPhotosPlugin = {
  savePhoto(opts: {
    url: string;
    album: string;
    takenAt: string;
    fileName: string;
  }): Promise<{ mediaStoreId: string }>;
};
