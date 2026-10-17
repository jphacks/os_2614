"use client";

import type { RevealDelay } from "@/domain/types";
import { Notice } from "@/components/ui/Notice";
import { Slider } from "@/components/ui/Slider";
import styles from "./RevealDelaySetting.module.css";

const STEPS: (number | null)[] = [0, 1, 3, 6, 12, 24, 72, null];

function labelFor(hours: number | null): string {
  if (hours === null) {
    return "非公開";
  }
  if (hours === 0) {
    return "すぐ";
  }
  if (hours < 24) {
    return `${hours}時間`;
  }
  return `${hours / 24}日`;
}

function indexFor(revealDelay: RevealDelay): number {
  const hours = revealDelay?.hours ?? null;
  const index = STEPS.findIndex((step) => step === hours);
  return index === -1 ? 2 : index;
}

export type RevealDelaySettingProps = {
  revealDelay: RevealDelay;
  onChange: (revealDelay: RevealDelay) => void;
};

export function RevealDelaySetting({
  revealDelay,
  onChange,
}: RevealDelaySettingProps) {
  const index = indexFor(revealDelay);

  return (
    <div className={styles.card}>
      <div className={styles.head}>
        <span className={styles.label}>相手に見えるまで</span>
        <span className={styles.value}>{labelFor(STEPS[index])}</span>
      </div>
      <Slider
        min={0}
        max={STEPS.length - 1}
        step={1}
        value={index}
        label="相手に見えるまで"
        marks={["すぐ", "1日", "非公開"]}
        onChange={(next) => {
          const hours = STEPS[next];
          onChange(hours === null ? null : { hours });
        }}
      />
      <Notice tone="info" icon="info">
        相手と設定がちがうときは、長いほうが使われます
      </Notice>
    </div>
  );
}
