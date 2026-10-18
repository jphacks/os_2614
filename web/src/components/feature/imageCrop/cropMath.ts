// 画像トリミングの座標計算。frame (表示枠) に対して画像を cover で敷き、
// zoom (>= 1) と中心座標 (画像ピクセル) で見える範囲を決める。

export type CropView = {
  /** 1 = 枠いっぱいに cover。大きいほど拡大。 */
  zoom: number;
  /** 枠の中心に来る画像上の座標 (画像ピクセル)。 */
  cx: number;
  cy: number;
};

export type Size = { width: number; height: number };

export const MAX_ZOOM = 4;

/** 枠を埋める最小の倍率 (zoom = 1 のとき)。 */
export function coverScale(image: Size, frame: Size): number {
  return Math.max(frame.width / image.width, frame.height / image.height);
}

/** zoom を範囲内に収め、枠から画像がはみ出さないよう中心を寄せる。 */
export function clampView(view: CropView, image: Size, frame: Size): CropView {
  const zoom = Math.min(MAX_ZOOM, Math.max(1, view.zoom));
  const scale = coverScale(image, frame) * zoom;
  const halfW = frame.width / scale / 2;
  const halfH = frame.height / scale / 2;
  return {
    zoom,
    cx: Math.min(image.width - halfW, Math.max(halfW, view.cx)),
    cy: Math.min(image.height - halfH, Math.max(halfH, view.cy)),
  };
}

/** 枠に見えている範囲 (画像ピクセル)。書き出しの drawImage 元矩形になる。 */
export function visibleRect(view: CropView, image: Size, frame: Size) {
  const scale = coverScale(image, frame) * view.zoom;
  const width = frame.width / scale;
  const height = frame.height / scale;
  return { x: view.cx - width / 2, y: view.cy - height / 2, width, height };
}
