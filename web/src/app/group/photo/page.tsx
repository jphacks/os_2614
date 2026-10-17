"use client";

// /group/photo は static export。`?id=` の Space (グループ / パーティ) へ写真を 1 枚
// 共有する。選んだ画像は POST /media (10 §4.4) で上げ、返った mediaId を
// POST /instances/{spaceId}/media (10 §4.4) に載せて写真投稿を作る。
// 失敗は toResult で ok / unavailable (404・405・501) / error に畳んで画面へ返す。

import { Suspense, useCallback } from "react";
import { useSearchParams } from "next/navigation";
import { ApiError } from "@/auth/errors";
import { postJson, toResult } from "@/data/api";
import { uploadMedia } from "@/components/feature/compose";
import {
  SpacePhotoShareScreen,
  type SpacePhotoShareResult,
} from "@/components/screens/SpacePhotoShareScreen";

/** POST /instances/{spaceId}/media の応答 (service.SharedMedia)。 */
type SharedMedia = { uri: string; spaceId: string; mediaId: string };

/** POST /media の失敗を画面の文言へ畳む。 */
function uploadMessage(error: ApiError): string {
  if (error.status === 413) {
    return "画像は 15MB までです";
  }
  if (error.status === 400) {
    return error.message || "この画像はアップロードできません";
  }
  return "画像をアップロードできませんでした";
}

/** POST /instances/{spaceId}/media の失敗を画面の文言へ畳む (404 は unavailable 側)。 */
function shareMessage(error: ApiError): string {
  if (error.status === 403) {
    return "この Space に写真を共有する権限がありません";
  }
  if (error.status >= 500) {
    return "写真の共有に失敗しました。時間をおいて試してください";
  }
  return error.message || "写真を共有できませんでした";
}

function ShareForm() {
  const spaceId = useSearchParams().get("id") ?? "";

  const onShare = useCallback(
    async (file: File): Promise<SpacePhotoShareResult> => {
      const upload = await toResult(() => uploadMedia(file));
      if (upload.status === "unavailable") {
        return { status: "unavailable" };
      }
      if (upload.status === "error") {
        return { status: "error", message: uploadMessage(upload.error) };
      }
      const share = await toResult(() =>
        postJson<SharedMedia>(`/instances/${encodeURIComponent(spaceId)}/media`, {
          mediaId: upload.data.id,
        }),
      );
      if (share.status === "ok") {
        return { status: "ok" };
      }
      if (share.status === "unavailable") {
        return { status: "unavailable" };
      }
      return { status: "error", message: shareMessage(share.error) };
    },
    [spaceId],
  );

  return <SpacePhotoShareScreen spaceId={spaceId} onShare={onShare} />;
}

export default function SpacePhotoSharePage() {
  // useSearchParams は Suspense 境界の内側でだけ使える (static export の prerender 用)。
  return (
    <Suspense fallback={null}>
      <ShareForm />
    </Suspense>
  );
}
