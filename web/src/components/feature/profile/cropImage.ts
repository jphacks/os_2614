export type CropSelection = {
  zoom: number;
  /** フレーム幅・高さに対する画像の移動量。右へ動かすと正。 */
  offsetX: number;
  offsetY: number;
};

const MAX_SOURCE_LONG_SIDE = 4096;

async function sourceBitmap(file: Blob): Promise<ImageBitmap> {
  const original = await createImageBitmap(file);
  const longSide = Math.max(original.width, original.height);
  if (longSide <= MAX_SOURCE_LONG_SIDE) return original;
  const scale = MAX_SOURCE_LONG_SIDE / longSide;
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.round(original.width * scale));
  canvas.height = Math.max(1, Math.round(original.height * scale));
  const context = canvas.getContext("2d");
  if (!context) {
    original.close();
    throw new Error("画像を読み込めませんでした");
  }
  context.drawImage(original, 0, 0, canvas.width, canvas.height);
  original.close();
  return createImageBitmap(canvas);
}

export async function cropImage(
  file: Blob,
  aspect: "avatar" | "banner",
  selection: CropSelection,
): Promise<Blob> {
  const bitmap = await sourceBitmap(file);
  const frameAspect = aspect === "avatar" ? 1 : 3;
  const maxWidth = aspect === "avatar" ? 1000 : 3000;
  const baseRegionWidth =
    bitmap.width / bitmap.height > frameAspect
      ? bitmap.height * frameAspect
      : bitmap.width;
  const baseRegionHeight = baseRegionWidth / frameAspect;
  const regionWidth = Math.min(bitmap.width, baseRegionWidth / selection.zoom);
  const regionHeight = Math.min(bitmap.height, baseRegionHeight / selection.zoom);
  const sourceX = Math.max(
    0,
    Math.min(
      bitmap.width - regionWidth,
      (bitmap.width - regionWidth) / 2 - selection.offsetX * regionWidth,
    ),
  );
  const sourceY = Math.max(
    0,
    Math.min(
      bitmap.height - regionHeight,
      (bitmap.height - regionHeight) / 2 - selection.offsetY * regionHeight,
    ),
  );
  const candidateWidth = Math.max(
    1,
    Math.floor(Math.min(maxWidth, regionWidth, regionHeight * frameAspect)),
  );
  const outputWidth =
    aspect === "banner" && candidateWidth >= 3
      ? Math.floor(candidateWidth / 3) * 3
      : candidateWidth;
  const outputHeight =
    aspect === "avatar" ? outputWidth : Math.max(1, Math.floor(outputWidth / 3));
  const canvas = document.createElement("canvas");
  canvas.width = outputWidth;
  canvas.height = outputHeight;
  const context = canvas.getContext("2d");
  if (!context) {
    bitmap.close();
    throw new Error("画像を切り抜けませんでした");
  }
  context.fillStyle = "white";
  context.fillRect(0, 0, canvas.width, canvas.height);
  context.drawImage(
    bitmap,
    sourceX,
    sourceY,
    regionWidth,
    regionHeight,
    0,
    0,
    outputWidth,
    outputHeight,
  );
  bitmap.close();
  const result = await new Promise<Blob | null>((resolve) =>
    canvas.toBlob(resolve, "image/jpeg", 0.9),
  );
  if (!result) throw new Error("画像を切り抜けませんでした");
  return result;
}
