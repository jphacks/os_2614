"use client";

import type { Party } from "@/domain/types";
import { useNow } from "@/hooks/useNow";
import { clockTime, elapsed } from "@/data/format";
import styles from "./PartyHero.module.css";

export type PartyHeroProps = {
  party: Party;
};

/** 仮パーティが本パーティになるまでの目安（08 §7 ConfirmAfter = 5 分）。 */
const CONFIRM_SEC = 5 * 60;

function clamp01(value: number): number {
  return Math.min(1, Math.max(0, value));
}

export function PartyHero({ party }: PartyHeroProps) {
  // 経過は backend の tentativeAt（仮パーティ開始）から数える。フロントで開始時刻を作らない。
  const start = Date.parse(party.tentativeAt);
  const tick = useNow(1000);
  const now = tick > 0 ? tick : Number.isFinite(start) ? start : 0;

  const elapsedSec = Number.isFinite(start) ? Math.max(0, (now - start) / 1000) : 0;
  const ratio = party.phase === "confirmed" ? 1 : clamp01(elapsedSec / CONFIRM_SEC);
  const percent = Math.round(ratio * 100);

  return (
    <section className={styles.hero}>
      <div className={styles.top}>
        <span className={styles.badge}>
          <span className={styles.dot} aria-hidden />
          {party.phase === "confirmed" ? "本パーティ" : "仮パーティ"}
        </span>
        <span className={styles.area}>
          {party.phase === "confirmed" && party.confirmedAt
            ? `${clockTime(party.confirmedAt)} に確定`
            : "確定待ち"}
        </span>
      </div>

      <div className={styles.timerBlock}>
        <span className={styles.label}>いっしょにいる時間</span>
        <span className={styles.timer}>
          {elapsed(party.tentativeAt, now)}
        </span>
      </div>

      <div
        className={styles.bar}
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        aria-label="本パーティまでの進み具合"
      >
        <span className={styles.thumb} style={{ left: `${percent}%` }} />
      </div>
    </section>
  );
}
