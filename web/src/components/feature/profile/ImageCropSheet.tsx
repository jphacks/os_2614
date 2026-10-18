"use client";

import { useEffect, useRef, useState, type PointerEvent } from "react";
import { Button } from "@/components/ui/Button";
import { Slider } from "@/components/ui/Slider";
import { cropImage, type CropSelection } from "./cropImage";
import styles from "./ImageCropSheet.module.css";

type ImageCropSheetProps = {
  file: File;
  kind: "avatar" | "banner";
  onCancel: () => void;
  onConfirm: (blob: Blob) => void;
};

function clampOffsets(
  frameWidth: number,
  frameHeight: number,
  zoom: number,
  imageRatio: number,
  offsetX: number,
  offsetY: number,
) {
  const coverWidth = Math.max(frameWidth, frameHeight * imageRatio);
  const coverHeight = Math.max(frameHeight, frameWidth / imageRatio);
  const maxX = Math.max(0, (coverWidth * zoom - frameWidth) / (2 * frameWidth));
  const maxY = Math.max(0, (coverHeight * zoom - frameHeight) / (2 * frameHeight));
  return {
    offsetX: Math.max(-maxX, Math.min(maxX, offsetX)),
    offsetY: Math.max(-maxY, Math.min(maxY, offsetY)),
  };
}

export function ImageCropSheet({ file, kind, onCancel, onConfirm }: ImageCropSheetProps) {
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const [selection, setSelection] = useState<CropSelection>({ zoom: 1, offsetX: 0, offsetY: 0 });
  const [imageSize, setImageSize] = useState({ width: 1, height: 1 });
  const [frameSize, setFrameSize] = useState({ width: 0, height: 0 });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const frameRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{ x: number; y: number; offsetX: number; offsetY: number } | null>(null);
  const cancelRef = useRef(onCancel);
  const busyRef = useRef(busy);

  useEffect(() => {
    cancelRef.current = onCancel;
    busyRef.current = busy;
  }, [busy, onCancel]);

  useEffect(() => {
    const url = URL.createObjectURL(file);
    // object URL は file と同期する外部リソースなので、effect から preview state を更新する。
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setPreviewUrl(url);
    return () => URL.revokeObjectURL(url);
  }, [file]);

  useEffect(() => {
    const frame = frameRef.current;
    if (!frame) return;
    const observer = new ResizeObserver(([entry]) => {
      setFrameSize({
        width: entry.contentRect.width,
        height: entry.contentRect.height,
      });
    });
    observer.observe(frame);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    dialogRef.current?.focus();
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busyRef.current) cancelRef.current();
    };
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, []);

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = { x: event.clientX, y: event.clientY, offsetX: selection.offsetX, offsetY: selection.offsetY };
  };

  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    if (!drag.current) return;
    const frameWidth = event.currentTarget.clientWidth;
    const frameHeight = event.currentTarget.clientHeight;
    const imageRatio = imageSize.width / imageSize.height;
    const clamped = clampOffsets(
      frameWidth,
      frameHeight,
      selection.zoom,
      imageRatio,
      drag.current.offsetX + (event.clientX - drag.current.x) / frameWidth,
      drag.current.offsetY + (event.clientY - drag.current.y) / frameHeight,
    );
    setSelection((current) => ({
      ...current,
      ...clamped,
    }));
  };

  const changeZoom = (zoom: number) => {
    const width = frameSize.width || 1;
    const height = frameSize.height || 1;
    const imageRatio = imageSize.width / imageSize.height;
    const clamped = clampOffsets(
      width,
      height,
      zoom,
      imageRatio,
      selection.offsetX,
      selection.offsetY,
    );
    setSelection({
      zoom,
      ...clamped,
    });
  };

  const confirm = async () => {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      onConfirm(await cropImage(file, kind, selection));
    } catch {
      setError("画像を読み込めませんでした。別の画像を選んでください。");
      setBusy(false);
    }
  };

  return (
    <div
      ref={dialogRef}
      className={styles.backdrop}
      role="dialog"
      aria-modal="true"
      aria-label={kind === "avatar" ? "アイコンを切り抜く" : "バナーを切り抜く"}
      tabIndex={-1}
    >
      <div className={styles.sheet}>
      <div
        ref={frameRef}
        className={`${styles.frame} ${kind === "avatar" ? styles.avatarFrame : styles.bannerFrame}`}
          data-crop-frame
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={() => { drag.current = null; }}
          onPointerCancel={() => { drag.current = null; }}
        >
          {previewUrl && frameSize.width > 0 && (
            // eslint-disable-next-line @next/next/no-img-element
            <img
              src={previewUrl}
              alt=""
              className={styles.cropImage}
              style={{
                width: Math.max(frameSize.width, frameSize.height * (imageSize.width / imageSize.height)),
                height: Math.max(frameSize.height, frameSize.width / (imageSize.width / imageSize.height)),
                left: "50%",
                top: "50%",
                transform: `translate(-50%, -50%) translate(${selection.offsetX * frameSize.width}px, ${selection.offsetY * frameSize.height}px) scale(${selection.zoom})`,
              }}
              onLoad={(event) => setImageSize({ width: event.currentTarget.naturalWidth, height: event.currentTarget.naturalHeight })}
              onError={() => setError("画像を読み込めませんでした。別の画像を選んでください。")}
            />
          )}
          <span className={styles.mask} aria-hidden />
        </div>
        <Slider min={1} max={3} step={0.01} value={selection.zoom} onChange={changeZoom} label="拡大率" marks={["1x", "3x"]} />
        <span className={styles.hint}>画像をドラッグして位置を調整</span>
        {error && <p className={styles.error} role="alert">{error}</p>}
        <div className={styles.actions}>
          <Button variant="secondary" size="sm" onClick={onCancel} disabled={busy}>キャンセル</Button>
          <Button variant="primary" size="sm" onClick={() => void confirm()} disabled={busy}>{busy ? "処理しています…" : "決定"}</Button>
        </div>
      </div>
    </div>
  );
}
