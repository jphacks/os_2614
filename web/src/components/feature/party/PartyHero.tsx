"use client";

import type { Party } from "@/domain/types";
import { useNow } from "@/hooks/useNow";
import { elapsed } from "@/data/format";
import styles from "./PartyHero.module.css";

export type PartyHeroProps = {
  party: Party;
};

const PROVISIONAL_START_SEC = 30;
const PROVISIONAL_SPAN_SEC = 270;
const ACTIVE_RATIO = 0.08;

function clamp(value: number): number {
  return Math.min(1, Math.max(0, value));
}

export function PartyHero({ party }: PartyHeroProps) {
  const start = Date.parse(party.startedAt);
  const tick = useNow(1000);
  const now = tick > 0 ? tick : Number.isFinite(start) ? start : 0;

  const elapsedSec = Number.isFinite(start) ? Math.max(0, (now - start) / 1000) : 0;
  const ratio =
    party.stage === "provisional"
      ? clamp((elapsedSec - PROVISIONAL_START_SEC) / PROVISIONAL_SPAN_SEC)
      : ACTIVE_RATIO;
  const percent = Math.round(ratio * 100);

  return (
    <section className={styles.hero}>
      <div className={styles.top}>
        <span className={styles.badge}>
          <span className={styles.dot} aria-hidden />
          {party.stage === "provisional" ? "仮パーティ" : "本パーティ"}
        </span>
        <span className={styles.area}>{party.areaLabel}</span>
      </div>

      <div className={styles.timerBlock}>
        <span className={styles.label}>いっしょにいる時間</span>
        <span className={styles.timer}>{elapsed(party.startedAt, now)}</span>
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
