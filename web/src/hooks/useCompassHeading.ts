"use client";

import { useEffect, useState } from "react";

/** iOS Safari / WKWebView だけが持つ、真北基準の方位。 */
type WebkitOrientationEvent = DeviceOrientationEvent & { webkitCompassHeading?: number };

/** これ未満の向きの変化では再描画しない (度)。 */
const MIN_DELTA_DEG = 2;
/** 指数平滑の係数。センサーの細かい揺れで泡が震えないようにする。 */
const SMOOTHING = 0.25;

/** a から b への最短の角度差 (-180〜180)。 */
function angleDelta(a: number, b: number): number {
  return ((b - a + 540) % 360) - 180;
}

/**
 * 端末の向いている方位 (度、北 0・時計回り)。センサーが無い / 取れない環境では null。
 *
 * Android (Chrome / WebView) は deviceorientationabsolute の alpha、iOS は
 * webkitCompassHeading を使う。どちらも無い環境 (デスクトップ) では null のまま。
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

    const onAbsolute = (event: DeviceOrientationEvent) => {
      // alpha は反時計回りなので、北 0・時計回りの方位に直す
      if (event.alpha !== null) update((360 - event.alpha) % 360);
    };

    const onRelative = (event: DeviceOrientationEvent) => {
      const compass = (event as WebkitOrientationEvent).webkitCompassHeading;
      if (typeof compass === "number") {
        update(compass);
      } else if (event.absolute && event.alpha !== null) {
        update((360 - event.alpha) % 360);
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
