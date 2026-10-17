"use client";

// 起動時に一度だけ出すスプラッシュ。キャラクター (public/brand/charactor.svg) の 4 つの円が
// ぽこぽこ現れ、目が瞬きして、全体がフェードアウトする。アニメーションは CSS だけで行い、
// 終了時に unmount する。

import { useState } from "react";
import styles from "./StartupSplash.module.css";

/** CSS の総再生時間 (ms) と揃える。 */
const TOTAL_MS = 1900;

export function StartupSplash() {
  const [done, setDone] = useState(false);
  if (done) {
    return null;
  }
  return (
    <div
      className={styles.root}
      aria-hidden="true"
      onAnimationEnd={(e) => {
        if (e.target === e.currentTarget) {
          setDone(true);
        }
      }}
      style={{ ["--splash-total" as string]: `${TOTAL_MS}ms` }}
    >
      <svg className={styles.character} viewBox="0 0 260 260" fill="none">
        <circle className={`${styles.blob} ${styles.b1}`} cx="117" cy="159" r="59" />
        <circle className={`${styles.blob} ${styles.b2}`} cx="197" cy="140" r="59" />
        <circle className={`${styles.blob} ${styles.b3}`} cx="64" cy="126" r="59" />
        <circle className={`${styles.blob} ${styles.b4}`} cx="148" cy="100" r="59" />
        <g className={styles.eyes}>
          <circle cx="96.5" cy="121.5" r="15.5" />
          <circle cx="169.5" cy="121.5" r="15.5" />
        </g>
      </svg>
    </div>
  );
}
