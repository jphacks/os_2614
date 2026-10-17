import { visibleRect, type CropView, type Size } from "./cropMath";

/** 選択範囲を out サイズに書き出した File にする。PNG は PNG、それ以外は JPEG。 */
export async function exportCrop(
  bitmap: ImageBitmap,
  view: CropView,
  frame: Size,
  out: Size,
  source: File,
): Promise<File> {
  const canvas = document.createElement("canvas");
  canvas.width = out.width;
  canvas.height = out.height;
  const ctx = canvas.getContext("2d");
  if (!ctx) {
    throw new Error("画像を処理できませんでした");
  }
  ctx.imageSmoothingQuality = "high";
  const rect = visibleRect(view, bitmap, frame);
  ctx.drawImage(
    bitmap,
    rect.x,
    rect.y,
    rect.width,
    rect.height,
    0,
    0,
    out.width,
    out.height,
  );
  const type = source.type === "image/png" ? "image/png" : "image/jpeg";
  const blob = await new Promise<Blob | null>((resolve) =>
    canvas.toBlob(resolve, type, 0.92),
  );
  if (!blob) {
    throw new Error("画像を書き出せませんでした");
  }
  const ext = type === "image/png" ? "png" : "jpg";
  const base = source.name.replace(/\.[^.]+$/, "") || "image";
  return new File([blob], `${base}.${ext}`, { type });
}
