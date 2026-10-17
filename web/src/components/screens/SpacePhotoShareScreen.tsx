"use client";

// Space (グループ / パーティ) へ写真を 1 枚共有する画面 (10 §4.4)。
// 画面は「選ぶ → プレビュー → 共有」の UI と、選択ファイルの事前検証だけを持つ。
// API (POST /media → POST /instances/{spaceId}/media) は page.tsx が担い、結果を
// SpacePhotoShareResult に畳んで返す。unavailable はエンドポイント未対応 (404・405・501)。

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
} from "react";
import Link from "next/link";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { ImagePlaceholder } from "@/components/ui/ImagePlaceholder";
import { Notice } from "@/components/ui/Notice";
import { ACCEPTED_MEDIA_TYPES, validateMediaFile } from "@/components/feature/compose";
import styles from "./SpacePhotoShareScreen.module.css";

/** 共有操作の結果。unavailable はエンドポイント未対応 (404・405・501)。 */
export type SpacePhotoShareResult =
  | { status: "ok" }
  | { status: "unavailable" }
  | { status: "error"; message: string };

export type SpacePhotoShareScreenProps = {
  /** 共有先の Space (クエリ `?id=` のグループ / パーティ)。 */
  spaceId: string;
  /** 共有の実行。page が POST /media → POST /instances/{spaceId}/media を行う。 */
  onShare: (file: File) => Promise<SpacePhotoShareResult>;
};

export function SpacePhotoShareScreen({
  spaceId,
  onShare,
}: SpacePhotoShareScreenProps) {
  const fileInputRef = useRef<HTMLInputElement>(null);
  // 現在のプレビュー URL。差し替え・削除・失敗・unmount で必ず解放する。
  const previewUrlRef = useRef<string | null>(null);
  const [file, setFile] = useState<File | null>(null);
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  // unmount で object URL を解放する (setState を伴わない後始末だけ)。
  useEffect(
    () => () => {
      if (previewUrlRef.current) {
        URL.revokeObjectURL(previewUrlRef.current);
      }
    },
    [],
  );

  const clearSelection = useCallback(() => {
    if (previewUrlRef.current) {
      URL.revokeObjectURL(previewUrlRef.current);
      previewUrlRef.current = null;
    }
    setFile(null);
    setPreviewUrl(null);
  }, []);

  const pick = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  const onFilesSelected = useCallback(
    (event: ChangeEvent<HTMLInputElement>) => {
      const selected = event.target.files?.[0];
      // 同じファイルを選び直せるように毎回クリアする。
      event.target.value = "";
      if (!selected) {
        return;
      }
      setSuccess(false);
      const check = validateMediaFile(selected);
      if (!check.ok) {
        clearSelection();
        setError(check.message);
        return;
      }
      if (previewUrlRef.current) {
        URL.revokeObjectURL(previewUrlRef.current);
      }
      const url = URL.createObjectURL(selected);
      previewUrlRef.current = url;
      setFile(selected);
      setPreviewUrl(url);
      setError(null);
    },
    [clearSelection],
  );

  const share = useCallback(async () => {
    if (!file || busy) {
      return;
    }
    setBusy(true);
    setError(null);
    setSuccess(false);
    const result = await onShare(file);
    setBusy(false);
    if (result.status === "ok") {
      clearSelection();
      setSuccess(true);
      return;
    }
    // 失敗した写真のプレビューは残さず、object URL を解放して選び直させる。
    clearSelection();
    setError(
      result.status === "unavailable" ? "写真の共有は準備中です" : result.message,
    );
  }, [busy, clearSelection, file, onShare]);

  return (
    <AppShell tab="nearby">
      <ScreenHeader
        title="写真を共有"
        back={spaceId ? `/group/?id=${spaceId}` : "/"}
        subtitle="グループのみんなに見えます"
      />

      <div className={styles.section}>
        {success && (
          <Notice tone="info" icon="check_circle" className={styles.success}>
            写真を共有しました。
            <Link className={styles.link} href={`/group/?id=${spaceId}`}>
              グループを見る
            </Link>
          </Notice>
        )}

        {error && (
          <Notice tone="warning" icon="error">
            {error}
          </Notice>
        )}

        <Card padding="md">
          <input
            ref={fileInputRef}
            type="file"
            accept={ACCEPTED_MEDIA_TYPES.join(",")}
            hidden
            data-testid="space-photo-input"
            onChange={onFilesSelected}
          />

          {previewUrl ? (
            <div className={styles.preview}>
              <ImagePlaceholder ratio="1/1" src={previewUrl} alt="" />
              <button
                type="button"
                className={styles.remove}
                aria-label="写真を削除"
                onClick={clearSelection}
              >
                <Icon name="close" size={16} />
              </button>
            </div>
          ) : (
            <button type="button" className={styles.picker} onClick={pick}>
              <Icon name="add_a_photo" size={32} />
              <span>写真を選ぶ</span>
            </button>
          )}
        </Card>

        <Button
          variant="primary"
          disabled={!file || busy}
          onClick={() => void share()}
        >
          {busy ? "共有中…" : "共有する"}
        </Button>

        <p className={styles.hint}>
          JPEG・PNG・WebP の画像を 15MB まで共有できます。
        </p>
      </div>
    </AppShell>
  );
}
