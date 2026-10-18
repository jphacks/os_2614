"use client";

import {
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
} from "react";
import styles from "./BubbleCanvas.module.css";

/**
 * 座標はワールド座標 (px)。原点 (0, 0) が初期状態でビューポート中央に来る。
 * 位置情報から描画する場合は「自分 = 原点」、方角と距離を x / y に変換して渡す想定。
 */
export type BubbleCanvasItem = {
  id: string;
  x: number;
  y: number;
  /** 直径 (px)。省略時は itemSize */
  size?: number;
  /** true なら rotation で回さない (原点の周りに固定して置くもの) */
  fixed?: boolean;
};

export type BubbleRenderState = {
  /** ビューポート中央に最も近いアイテム (lens が効いている時のみ) */
  focused: boolean;
};

export type BubbleCanvasProps<T extends BubbleCanvasItem> = {
  items: readonly T[];
  renderItem: (item: T, state: BubbleRenderState) => ReactNode;
  itemSize?: number;
  /**
   * 中央を拡大し周囲を押し広げるレンズ効果。
   * - "on-interact": 最初にドラッグするまでは等倍 (How We Feel と同じ挙動)
   * - "always": 常に有効
   * - "off": 無効
   */
  lens?: "on-interact" | "always" | "off";
  /** レンズの効く半径 (px)。省略時はビューポート短辺の半分 */
  lensRadius?: number;
  /** 中央のアイテムの拡大率 */
  focusScale?: number;
  /** レンズ外のアイテムの縮小率 */
  edgeScale?: number;
  /** 中央付近のアイテムを外側へ押し広げる量 (0 で押し広げない) */
  spread?: number;
  /**
   * 原点を中心にアイテムを回す角度 (度、時計回り)。変わると最短方向へなめらかに回る。
   * 端末の向きに合わせて周りを回すときに使う。
   */
  rotation?: number;
  onFocusChange?: (id: string | null) => void;
  onItemPress?: (item: T) => void;
  /**
   * true を返したアイテムは、タップでまず中央へ寄せてフォーカスする。
   * フォーカス済みのときにもう一度タップすると onItemPress を呼ぶ。
   */
  focusOnPress?: (item: T) => boolean;
  className?: string;
  ariaLabel?: string;
};

type Config = Required<
  Pick<
    BubbleCanvasProps<BubbleCanvasItem>,
    "itemSize" | "lens" | "focusScale" | "edgeScale" | "spread"
  >
> & {
  lensRadius?: number;
  rotation: number;
  items: readonly BubbleCanvasItem[];
  onFocusChange?: (id: string | null) => void;
};

type Sim = {
  width: number;
  height: number;
  ox: number;
  oy: number;
  vx: number;
  vy: number;
  /** キーボードフォーカス等でアニメーション移動する先 */
  target: { x: number; y: number } | null;
  /** target へ寄せている間のフォーカス先。途中で横切る泡にフォーカスを渡さない */
  targetId: string | null;
  pointerId: number | null;
  startX: number;
  startY: number;
  lastX: number;
  lastY: number;
  lastT: number;
  moved: boolean;
  suppressClick: boolean;
  engaged: boolean;
  lens: number;
  focusedId: string | null;
  /** いま描いている回転角 (度)。config.rotation へ寄せていく */
  angle: number;
  /** いま描いている各アイテムの位置 (回転前)。座標が変わると、ここから新しい位置へ寄せていく */
  shown: Map<string, { x: number; y: number }>;
  raf: number;
  prevT: number;
};

const TAP_SLOP = 6;
const RUBBER_BAND = 0.35;
const FRICTION_PER_MS = 0.995;
const MIN_VELOCITY = 0.01;
/** 回転を目標へ寄せる 1 フレームあたりの割合 */
const ROTATION_EASE = 0.08;
/** アイテムの座標が変わったとき、新しい位置へ寄せる 1 フレームあたりの割合 */
const MOVE_EASE = 0.07;

/** a から b への最短の角度差 (-180〜180) */
function angleDelta(a: number, b: number) {
  return ((((b - a) % 360) + 540) % 360) - 180;
}

/** rotation (度) を反映した座標 */
function rotated(item: BubbleCanvasItem, angle: number) {
  if (item.fixed || angle === 0) return { x: item.x, y: item.y };
  const rad = (angle * Math.PI) / 180;
  const cos = Math.cos(rad);
  const sin = Math.sin(rad);
  return { x: item.x * cos - item.y * sin, y: item.x * sin + item.y * cos };
}

function bounds(items: readonly BubbleCanvasItem[], angle: number) {
  if (items.length === 0) return { minX: 0, maxX: 0, minY: 0, maxY: 0 };
  let minX = Infinity;
  let maxX = -Infinity;
  let minY = Infinity;
  let maxY = -Infinity;
  for (const item of items) {
    const { x, y } = rotated(item, angle);
    minX = Math.min(minX, x);
    maxX = Math.max(maxX, x);
    minY = Math.min(minY, y);
    maxY = Math.max(maxY, y);
  }
  return { minX, maxX, minY, maxY };
}

/** offset の許容範囲: ビューポート中央が常にアイテムの外接矩形内に収まる */
function clampOffset(
  items: readonly BubbleCanvasItem[],
  angle: number,
  ox: number,
  oy: number,
) {
  const b = bounds(items, angle);
  return {
    x: Math.min(-b.minX, Math.max(-b.maxX, ox)),
    y: Math.min(-b.minY, Math.max(-b.maxY, oy)),
  };
}

export function BubbleCanvas<T extends BubbleCanvasItem>({
  items,
  renderItem,
  itemSize = 96,
  lens = "on-interact",
  lensRadius,
  focusScale = 1.3,
  edgeScale = 0.9,
  spread = 0.35,
  rotation = 0,
  onFocusChange,
  onItemPress,
  focusOnPress,
  className,
  ariaLabel,
}: BubbleCanvasProps<T>) {
  const viewportRef = useRef<HTMLDivElement>(null);
  const itemEls = useRef(new Map<string, HTMLDivElement>());
  const config = useRef<Config>({
    items,
    itemSize,
    lens,
    lensRadius,
    focusScale,
    edgeScale,
    spread,
    rotation,
    onFocusChange,
  });
  const sim = useRef<Sim>({
    width: 0,
    height: 0,
    ox: 0,
    oy: 0,
    vx: 0,
    vy: 0,
    target: null,
    targetId: null,
    pointerId: null,
    startX: 0,
    startY: 0,
    lastX: 0,
    lastY: 0,
    lastT: 0,
    moved: false,
    suppressClick: false,
    engaged: false,
    lens: lens === "always" ? 1 : 0,
    focusedId: null,
    angle: rotation,
    shown: new Map(),
    raf: 0,
    prevT: 0,
  });
  const [focusedId, setFocusedId] = useState<string | null>(null);

  // 毎フレーム React を再レンダリングしないよう、transform は DOM に直接書く
  const paint = () => {
    const s = sim.current;
    const c = config.current;
    const radius = c.lensRadius ?? Math.min(s.width, s.height) / 2;
    const t = s.lens;
    let nearestId: string | null = null;
    let nearestD = Infinity;

    for (const item of c.items) {
      const el = itemEls.current.get(item.id);
      if (!el) continue;
      const at = s.shown.get(item.id);
      const pos = rotated(at ? { ...item, x: at.x, y: at.y } : item, s.angle);
      const px = pos.x + s.ox;
      const py = pos.y + s.oy;
      const d = Math.hypot(px, py);
      // 中央で 1、離れるほど 0 に近づく
      const f = radius > 0 ? Math.exp(-(d * d) / (2 * radius * radius)) : 0;
      const push = 1 + c.spread * f * t;
      const scale =
        1 + t * ((c.focusScale - 1) * f + (c.edgeScale - 1) * (1 - f));
      const size = item.size ?? c.itemSize;
      const sx = px * push;
      const sy = py * push;
      const half = (size * scale) / 2;
      const visible =
        Math.abs(sx) - half < s.width / 2 && Math.abs(sy) - half < s.height / 2;

      el.style.transform = `translate3d(${s.width / 2 + sx - size / 2}px, ${s.height / 2 + sy - size / 2}px, 0) scale(${scale})`;
      el.style.visibility = visible ? "" : "hidden";
      el.style.zIndex = String(Math.round(f * 1000));

      if (d < nearestD) {
        nearestD = d;
        nearestId = item.id;
      }
    }

    const nextFocus =
      s.engaged || c.lens === "always" ? (s.targetId ?? nearestId) : null;
    if (nextFocus !== s.focusedId) {
      s.focusedId = nextFocus;
      setFocusedId(nextFocus);
      c.onFocusChange?.(nextFocus);
    }
  };

  const step = (now: number) => {
    const s = sim.current;
    const c = config.current;
    const dt = Math.min(64, s.prevT ? now - s.prevT : 16);
    s.prevT = now;
    let active = false;

    if (s.pointerId === null) {
      const clamped = clampOffset(c.items, s.angle, s.ox, s.oy);
      if (s.target) {
        s.ox += (s.target.x - s.ox) * 0.2;
        s.oy += (s.target.y - s.oy) * 0.2;
        if (Math.hypot(s.target.x - s.ox, s.target.y - s.oy) < 0.5) {
          s.ox = s.target.x;
          s.oy = s.target.y;
          s.target = null;
          s.targetId = null;
        } else {
          active = true;
        }
      } else if (clamped.x !== s.ox || clamped.y !== s.oy) {
        // 範囲外: 慣性を捨ててバネで戻す
        s.vx = 0;
        s.vy = 0;
        s.ox += (clamped.x - s.ox) * 0.2;
        s.oy += (clamped.y - s.oy) * 0.2;
        if (Math.hypot(clamped.x - s.ox, clamped.y - s.oy) < 0.5) {
          s.ox = clamped.x;
          s.oy = clamped.y;
        } else {
          active = true;
        }
      } else if (Math.hypot(s.vx, s.vy) > MIN_VELOCITY) {
        s.ox += s.vx * dt;
        s.oy += s.vy * dt;
        const decay = Math.pow(FRICTION_PER_MS, dt);
        s.vx *= decay;
        s.vy *= decay;
        active = true;
      }
    }

    // 並びが入れ替わったアイテムは、前の位置から新しい位置へ動かす。初めて出るものはその場に置く
    const live = new Set<string>();
    for (const item of c.items) {
      live.add(item.id);
      const at = s.shown.get(item.id);
      if (!at) {
        s.shown.set(item.id, { x: item.x, y: item.y });
      } else if (Math.hypot(item.x - at.x, item.y - at.y) > 0.3) {
        at.x += (item.x - at.x) * MOVE_EASE;
        at.y += (item.y - at.y) * MOVE_EASE;
        active = true;
      } else {
        at.x = item.x;
        at.y = item.y;
      }
    }
    for (const id of s.shown.keys()) if (!live.has(id)) s.shown.delete(id);

    const turn = angleDelta(s.angle, c.rotation);
    if (Math.abs(turn) > 0.05) {
      s.angle += turn * ROTATION_EASE;
      active = true;
    } else {
      s.angle = c.rotation;
    }

    const lensTarget = c.lens === "off" ? 0 : s.engaged || c.lens === "always" ? 1 : 0;
    if (Math.abs(lensTarget - s.lens) > 0.001) {
      s.lens += (lensTarget - s.lens) * 0.15;
      active = true;
    } else {
      s.lens = lensTarget;
    }

    paint();
    s.raf = active ? requestAnimationFrame(step) : 0;
    if (!active) s.prevT = 0;
  };

  const kick = () => {
    const s = sim.current;
    if (s.raf === 0) s.raf = requestAnimationFrame(step);
  };

  useLayoutEffect(() => {
    config.current = {
      items,
      itemSize,
      lens,
      lensRadius,
      focusScale,
      edgeScale,
      spread,
      rotation,
      onFocusChange,
    };
    paint();
    kick();
  });

  useLayoutEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const s = sim.current;
    const measure = () => {
      const rect = viewport.getBoundingClientRect();
      s.width = rect.width;
      s.height = rect.height;
    };
    measure();
    paint();
    const observer = new ResizeObserver(() => {
      measure();
      kick();
    });
    observer.observe(viewport);

    // トラックパッド / マウスホイールでも移動できるように
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      s.target = null;
      s.targetId = null;
      s.engaged = true;
      s.ox -= event.deltaX;
      s.oy -= event.deltaY;
      kick();
    };
    viewport.addEventListener("wheel", onWheel, { passive: false });

    return () => {
      observer.disconnect();
      viewport.removeEventListener("wheel", onWheel);
      cancelAnimationFrame(s.raf);
      s.raf = 0;
    };
    // paint / kick は ref だけを触るので依存に含めない
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handlePointerDown = (event: PointerEvent<HTMLDivElement>) => {
    const s = sim.current;
    if (s.pointerId !== null) return;
    s.pointerId = event.pointerId;
    s.startX = s.lastX = event.clientX;
    s.startY = s.lastY = event.clientY;
    s.lastT = event.timeStamp;
    s.vx = 0;
    s.vy = 0;
    s.target = null;
    s.targetId = null;
    s.moved = false;
    s.suppressClick = false;
  };

  const handlePointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const s = sim.current;
    if (event.pointerId !== s.pointerId) return;
    if (!s.moved) {
      if (
        Math.hypot(event.clientX - s.startX, event.clientY - s.startY) <
        TAP_SLOP
      ) {
        return;
      }
      // タップ判定を抜けてからキャプチャする (中身のボタンのクリックを殺さない)
      s.moved = true;
      s.engaged = true;
      event.currentTarget.setPointerCapture(event.pointerId);
      event.currentTarget.dataset.dragging = "";
    }

    const dx = event.clientX - s.lastX;
    const dy = event.clientY - s.lastY;
    const dt = Math.max(1, event.timeStamp - s.lastT);
    const clamped = clampOffset(config.current.items, s.angle, s.ox, s.oy);
    s.ox += clamped.x !== s.ox ? dx * RUBBER_BAND : dx;
    s.oy += clamped.y !== s.oy ? dy * RUBBER_BAND : dy;
    s.vx = s.vx * 0.2 + (dx / dt) * 0.8;
    s.vy = s.vy * 0.2 + (dy / dt) * 0.8;
    s.lastX = event.clientX;
    s.lastY = event.clientY;
    s.lastT = event.timeStamp;
    kick();
  };

  const handlePointerEnd = (event: PointerEvent<HTMLDivElement>) => {
    const s = sim.current;
    if (event.pointerId !== s.pointerId) return;
    s.pointerId = null;
    if (s.moved) {
      s.suppressClick = true;
      delete event.currentTarget.dataset.dragging;
      // 指を止めてから離した場合は慣性を付けない
      if (event.timeStamp - s.lastT > 80 || event.type === "pointercancel") {
        s.vx = 0;
        s.vy = 0;
      }
    }
    kick();
  };

  const handleClickCapture = (event: MouseEvent<HTMLDivElement>) => {
    const s = sim.current;
    if (s.suppressClick) {
      s.suppressClick = false;
      event.preventDefault();
      event.stopPropagation();
    }
  };

  const centerOn = (item: BubbleCanvasItem) => {
    const s = sim.current;
    s.engaged = true;
    s.vx = 0;
    s.vy = 0;
    const pos = rotated(item, s.angle);
    s.target = { x: -pos.x, y: -pos.y };
    s.targetId = item.id;
    kick();
  };

  const press = (item: T) => {
    if (item.id !== focusedId && focusOnPress?.(item)) {
      centerOn(item);
      return;
    }
    onItemPress?.(item);
  };

  const classes = [styles.viewport, className].filter(Boolean).join(" ");

  return (
    <div
      ref={viewportRef}
      className={classes}
      role="group"
      aria-label={ariaLabel}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerEnd}
      onPointerCancel={handlePointerEnd}
      onClickCapture={handleClickCapture}
    >
      {items.map((item) => {
        const size = item.size ?? itemSize;
        const focused = item.id === focusedId;
        return (
          <div
            key={item.id}
            ref={(el) => {
              if (!el) return;
              itemEls.current.set(item.id, el);
              return () => {
                itemEls.current.delete(item.id);
              };
            }}
            className={styles.item}
            style={{ width: size, height: size }}
            data-focused={focused || undefined}
            role={onItemPress ? "button" : undefined}
            tabIndex={onItemPress ? 0 : undefined}
            onFocus={(event) => {
              // タップ時のフォーカスでは動かさず、キーボード移動時だけ中央へ寄せる
              if (event.currentTarget.matches(":focus-visible")) centerOn(item);
            }}
            onClick={onItemPress ? () => press(item) : undefined}
            onKeyDown={
              onItemPress
                ? (event: KeyboardEvent<HTMLDivElement>) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      press(item);
                    }
                  }
                : undefined
            }
          >
            {renderItem(item, { focused })}
          </div>
        );
      })}
    </div>
  );
}
