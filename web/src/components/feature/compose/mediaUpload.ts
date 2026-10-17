// Compose の画像アップロード（`POST /media`、multipart、10 §4.4）。
// 送信は型付きクライアントの multipart 経路（data/api.ts の postForm）に乗せる。

import { postForm } from "@/data/api";
import type { MediaUpload } from "@/data/dto";

/**
 * `POST /media` へ 1 枚送る。multipart の `file` に載せ、`takenAt`（RFC 3339）は
 * 分かるときだけ添える。応答は `{id,width,height}`。署名付き URL は返らないので、
 * プレビューは呼び出し側が `URL.createObjectURL` で作る。
 */
export async function uploadMedia(
  file: File,
  opts: { takenAt?: string; signal?: AbortSignal } = {},
): Promise<MediaUpload> {
  const form = new FormData();
  form.append("file", file);
  if (opts.takenAt) {
    form.append("takenAt", opts.takenAt);
  }
  return postForm<MediaUpload>("/media", form, { signal: opts.signal });
}
