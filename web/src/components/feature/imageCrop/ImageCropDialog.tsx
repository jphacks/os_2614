"use client";

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent,
  type WheelEvent,
} from "react";
import { Button } from "@/components/ui/Button";
import { Notice } from "@/components/ui/Notice";
import { Slider } from "@/components/ui/Slider";
import {
  MAX_ZOOM,
  clampView,
  coverScale,
  type CropView,
  type Size,
} from "./cropMath";
import { exportCrop } from "./exportCrop";
import styles from "./ImageCropDialog.module.css";

export type ImageCropKind = "avatar" | "banner";

const KINDS = {
  avatar: { title: "アイコンを調整", aspect: 1, out: { width: 512, height: 512 } },
  banner: { title: "バナーを調整", aspect: 2.5, out: { width: 1500, height: 600 } },
} satisfies Record<
  ImageCropKind,
  { title: string; aspect: number; out: Size }
>;

export type ImageCropDialogProps = {
  kind: ImageCropKind;
  file: File;
  onCancel: () => void;
  /** トリミング済みの File を渡す。送信中は busy を true にしてボタンを止める。 */
  onConfirm: (file: File) => void;
  busy?: boolean;
};

export function ImageCropDialog({
  kind,
  file,
  onCancel,
  onConfirm,
  busy = false,
}: ImageCropDialogProps) {
  const { title, aspect, out } = KINDS[kind];
  const frameRef = useRef<HTMLDivElement>(null);
  const [bitmap, setBitmap] = useState<ImageBitmap | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [frame, setFrame] = useState<Size>({ width: 0, height: 0 });
  const [view, setView] = useState<CropView | null>(null);
  const [exportError, setExportError] = useState<string | null>(null);
  const [exporting, setExporting] = useState(false);
  const drag = useRef<{ x: number; y: number; cx: number; cy: number } | null>(
    null,
  );
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const pinch = useRef<{ distance: number; zoom: number } | null>(null);

  // useMemo で作ると StrictMode の再マウントで revoke 済みの URL が残るので、effect で作り直す。
  const [url, setUrl] = useState<string | null>(null);
  useEffect(() => {
    const next = URL.createObjectURL(file);
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setUrl(next);
    return () => URL.revokeObjectURL(next);
  }, [file]);

  useEffect(() => {
    let cancelled = false;
    createImageBitmap(file, { imageOrientation: "from-image" }).then(
      (next) => {
        if (cancelled) {
          next.close();
          return;
        }
        setBitmap(next);
      },
      () => {
        if (!cancelled) {
          setLoadError(true);
        }
      },
    );
    return () => {
      cancelled = true;
    };
  }, [file]);

  useEffect(() => () => bitmap?.close(), [bitmap]);

  // 枠のサイズ (CSS ピクセル) を追う。向きの変化や回転でも崩れない。
  useLayoutEffect(() => {
    const el = frameRef.current;
    if (!el) {
      return;
    }
    const measure = () =>
      setFrame({ width: el.clientWidth, height: el.clientHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [bitmap]);

  const image = useMemo<Size | null>(
    () => (bitmap ? { width: bitmap.width, height: bitmap.height } : null),
    [bitmap],
  );
  const ready = image !== null && frame.width > 0;

  // 初期位置は中央・等倍。以降のズームは枠中心を保つ。
  const current: CropView | null = ready
    ? clampView(
        view ?? { zoom: 1, cx: image.width / 2, cy: image.height / 2 },
        image,
        frame,
      )
    : null;

  const update = useCallback(
    (next: Partial<CropView>) => {
      if (!image || frame.width === 0) {
        return;
      }
      setView((prev) =>
        clampView(
          {
            ...(prev ?? { zoom: 1, cx: image.width / 2, cy: image.height / 2 }),
            ...next,
          },
          image,
          frame,
        ),
      );
    },
    [image, frame],
  );

  const scale = ready ? coverScale(image, frame) * (current?.zoom ?? 1) : 1;

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (!current) {
      return;
    }
    event.currentTarget.setPointerCapture(event.pointerId);
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    if (pointers.current.size === 1) {
      drag.current = {
        x: event.clientX,
        y: event.clientY,
        cx: current.cx,
        cy: current.cy,
      };
      pinch.current = null;
    } else if (pointers.current.size === 2) {
      const [a, b] = [...pointers.current.values()];
      pinch.current = {
        distance: Math.hypot(a.x - b.x, a.y - b.y),
        zoom: current.zoom,
      };
      drag.current = null;
    }
  };

  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    if (!pointers.current.has(event.pointerId)) {
      return;
    }
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    if (pinch.current && pointers.current.size >= 2) {
      const [a, b] = [...pointers.current.values()];
      const distance = Math.hypot(a.x - b.x, a.y - b.y);
      if (pinch.current.distance > 0) {
        update({ zoom: pinch.current.zoom * (distance / pinch.current.distance) });
      }
      return;
    }
    if (drag.current) {
      update({
        cx: drag.current.cx - (event.clientX - drag.current.x) / scale,
        cy: drag.current.cy - (event.clientY - drag.current.y) / scale,
      });
    }
  };

  const onPointerEnd = (event: PointerEvent<HTMLDivElement>) => {
    pointers.current.delete(event.pointerId);
    pinch.current = null;
    // 指が 1 本残ったら、その位置から改めてドラッグを始める。
    const rest = [...pointers.current.values()][0];
    drag.current =
      rest && current
        ? { x: rest.x, y: rest.y, cx: current.cx, cy: current.cy }
        : null;
  };

  const onWheel = (event: WheelEvent<HTMLDivElement>) => {
    if (!current) {
      return;
    }
    update({ zoom: current.zoom * Math.exp(-event.deltaY * 0.002) });
  };

  const onKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (!current) {
      return;
    }
    const step = 20 / scale;
    const moves: Record<string, Partial<CropView>> = {
      ArrowLeft: { cx: current.cx - step },
      ArrowRight: { cx: current.cx + step },
      ArrowUp: { cy: current.cy - step },
      ArrowDown: { cy: current.cy + step },
      "+": { zoom: current.zoom * 1.1 },
      "=": { zoom: current.zoom * 1.1 },
      "-": { zoom: current.zoom / 1.1 },
    };
    const move = moves[event.key];
    if (move) {
      event.preventDefault();
      update(move);
    }
  };

  const confirm = async () => {
    if (!bitmap || !current || exporting || busy) {
      return;
    }
    setExporting(true);
    setExportError(null);
    try {
      onConfirm(await exportCrop(bitmap, current, frame, out, file));
    } catch (error) {
      setExportError(
        error instanceof Error ? error.message : "画像を処理できませんでした",
      );
    } finally {
      setExporting(false);
    }
  };

  const disabled = !current || exporting || busy;

  return (
    <div className={styles.overlay} role="dialog" aria-modal="true" aria-label={title}>
      <div className={styles.dialog} data-testid="image-crop-dialog">
        <h2 className={styles.title}>{title}</h2>

        <div
          ref={frameRef}
          className={`${styles.frame} ${kind === "avatar" ? styles.round : ""}`}
          style={{ aspectRatio: String(aspect) }}
          tabIndex={0}
          role="application"
          aria-label="ドラッグで位置、ピンチやスライダーで大きさを調整"
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerEnd}
          onPointerCancel={onPointerEnd}
          onWheel={onWheel}
          onKeyDown={onKeyDown}
        >
          {current && image && url && (
            // eslint-disable-next-line @next/next/no-img-element
            <img
              className={styles.image}
              src={url}
              alt=""
              draggable={false}
              style={{
                width: image.width * scale,
                height: image.height * scale,
                transform: `translate(${frame.width / 2 - current.cx * scale}px, ${
                  frame.height / 2 - current.cy * scale
                }px)`,
              }}
            />
          )}
          {!current && !loadError && <span className={styles.loading}>読み込み中…</span>}
          <span className={styles.grid} aria-hidden />
        </div>

        {kind === "avatar" && (
          <p className={styles.hint}>円の内側がアイコンとして表示されます</p>
        )}
        {kind === "banner" && (
          <p className={styles.hint}>
            端末の幅によって左右が少し切れることがあります
          </p>
        )}

        {loadError ? (
          <Notice tone="warning" icon="error">
            画像を読み込めませんでした
          </Notice>
        ) : (
          <div className={styles.zoomRow}>
            <span className={styles.zoomLabel}>大きさ</span>
            <Slider
              min={1}
              max={MAX_ZOOM}
              step={0.01}
              value={current?.zoom ?? 1}
              label="拡大率"
              onChange={(zoom) => update({ zoom })}
            />
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setView(null)}
              disabled={disabled}
            >
              リセット
            </Button>
          </div>
        )}

        {exportError && (
          <div role="alert">
            <Notice tone="warning" icon="error">
              {exportError}
            </Notice>
          </div>
        )}

        <div className={styles.actions}>
          <Button variant="secondary" onClick={onCancel} disabled={exporting || busy}>
            キャンセル
          </Button>
          <Button variant="primary" onClick={() => void confirm()} disabled={disabled}>
            {busy || exporting ? "処理しています…" : "決定"}
          </Button>
        </div>
      </div>
    </div>
  );
}
