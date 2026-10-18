"use client";

import { useEffect, useState } from "react";

/** iOS Safari / WKWebView だけが持つ、真北基準の方位。 */
type WebkitOrientationEvent = DeviceOrientationEvent & { webkitCompassHeading?: number };

/** これ未満の向きの変化では再描画しない (度)。 */
const MIN_DELTA_DEG = 2;
/** 指数平滑の係数。センサーの細かい揺れで泡が震えないようにする。 */
const SMOOTHING = 0.25;

/**
 * 前後の傾き (beta) がこれ以上なら端末を立てているとみなし、背面の向きを方位にする (度)。
 * それより寝かせているときは上端の向きを使う。
 */
const UPRIGHT_BETA_DEG = 60;

/** a から b への最短の角度差 (-180〜180)。 */
function angleDelta(a: number, b: number): number {
  return ((b - a + 540) % 360) - 180;
}

/**
 * deviceorientation の 3 軸 (北基準、Z-X'-Y'') から、画面を見ている人が向いている方位 (度) を求める。
 *
 * 端末の上端 (y 軸) を水平面へ射影した向きは、回転行列を展開すると左右の傾き (gamma) に
 * よらず 360 - alpha になる。ただし立てる (beta → 90°) と射影が潰れて alpha が暴れるので、
 * そのときは W3C の例 (Device Orientation §A.2) と同じく背面 (-z 軸) の向きを使う。
 * 逆に寝かせているときに背面を使うと、背面がほぼ真下を向くため gamma で大きく振れる。
 */
function headingFrom(alpha: number, beta: number, gamma: number): number {
  if (Math.abs(beta) < UPRIGHT_BETA_DEG) {
    return (360 - alpha) % 360;
  }
  const rad = Math.PI / 180;
  const cA = Math.cos(alpha * rad);
  const sA = Math.sin(alpha * rad);
  const sB = Math.sin(beta * rad);
  const cG = Math.cos(gamma * rad);
  const sG = Math.sin(gamma * rad);
  // 背面 (-z 軸) を水平面へ射影した向き
  const rA = -cA * sG - sA * sB * cG;
  const rB = -sA * sG + cA * sB * cG;
  return ((Math.atan2(rA, rB) / rad) + 360) % 360;
}

/** 画面の回転 (横向き) のぶんだけ方位を戻す。縦固定なら 0。 */
function screenAngle(): number {
  return typeof screen !== "undefined" && screen.orientation ? screen.orientation.angle : 0;
}

/**
 * 端末の向いている方位 (度、北 0・時計回り)。センサーが無い / 取れない環境では null。
 *
 * Android (Chrome / WebView) は deviceorientationabsolute の 3 軸から傾きを補正して求め、
 * iOS は webkitCompassHeading を使う。どちらも無い環境 (デスクトップ) では null のまま。
 */
export function useCompassHeading(): number | null {
  const [heading, setHeading] = useState<number | null>(null);

  useEffect(() => {
    if (typeof window === "undefined" || !("DeviceOrientationEvent" in window)) {
      return;
    }

    let smoothed: number | null = null;
    let published: number | null = null;
    let raf = 0;

    const publish = () => {
      raf = 0;
      if (smoothed === null) return;
      if (published !== null && Math.abs(angleDelta(published, smoothed)) < MIN_DELTA_DEG) {
        return;
      }
      published = smoothed;
      setHeading(Math.round(smoothed));
    };

    const update = (raw: number) => {
      smoothed =
        smoothed === null ? raw : (smoothed + angleDelta(smoothed, raw) * SMOOTHING + 360) % 360;
      if (raf === 0) raf = requestAnimationFrame(publish);
    };

    const fromEvent = (event: DeviceOrientationEvent) => {
      if (event.alpha === null || event.beta === null || event.gamma === null) return;
      update((headingFrom(event.alpha, event.beta, event.gamma) + screenAngle()) % 360);
    };

    const onAbsolute = (event: DeviceOrientationEvent) => fromEvent(event);

    const onRelative = (event: DeviceOrientationEvent) => {
      const compass = (event as WebkitOrientationEvent).webkitCompassHeading;
      if (typeof compass === "number") {
        // iOS は端末の傾きを補正済みの方位を返す
        update(compass);
      } else if (event.absolute) {
        fromEvent(event);
      }
    };

    window.addEventListener("deviceorientationabsolute", onAbsolute);
    window.addEventListener("deviceorientation", onRelative);
    return () => {
      window.removeEventListener("deviceorientationabsolute", onAbsolute);
      window.removeEventListener("deviceorientation", onRelative);
      cancelAnimationFrame(raf);
    };
  }, []);

  return heading;
}
