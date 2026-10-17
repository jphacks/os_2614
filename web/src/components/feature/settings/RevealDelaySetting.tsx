"use client";

import type { EncounterMode, Settings } from "@/domain/types";
import { Notice } from "@/components/ui/Notice";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import styles from "./RevealDelaySetting.module.css";
import { Pressable } from "@/components/ui/Pressable";

/**
 * backend 06 §5.1 の遅延プリセット (秒)。UI はここに無い中間値 (3h/12h など)
 * を選ばせない。stored な `encounterDelaySec` は 60〜604800 の任意値を取りうるが、
 * 画面から選べるのはこの 4 値だけ。
 */
export const ENCOUNTER_DELAY_PRESETS = [3600, 21600, 86400, 259200] as const;

/** 公開モード (06 §5.1)。 */
const MODES: readonly { value: EncounterMode; label: string }[] = [
  { value: "immediate", label: "すぐ" },
  { value: "delayed", label: "一定時間後" },
  { value: "private", label: "非公開" },
];

const MODE_LABELS: Record<EncounterMode, string> = {
  immediate: "すぐ",
  delayed: "一定時間後",
  private: "非公開",
};

const PRESET_LABELS: Record<number, string> = {
  3600: "1時間",
  21600: "6時間",
  86400: "1日",
  259200: "3日",
};

function isPreset(sec: number): boolean {
  return sec in PRESET_LABELS;
}

/**
 * 右側に出す現在値。vocabulary 外の stored 値は数字を出さず「一定時間後」に留める
 * (backend に無い遅延値を UI が断定しないため)。
 */
function valueText(mode: EncounterMode, delaySec: number): string {
  if (mode !== "delayed") {
    return MODE_LABELS[mode];
  }
  return PRESET_LABELS[delaySec] ?? "一定時間後";
}

/** `PATCH /me/settings` に載る部分更新。この部品はこの 2 キーだけを通知する。 */
export type RevealPatch = Partial<
  Pick<Settings, "encounterMode" | "encounterDelaySec">
>;

export type RevealDelaySettingProps = {
  encounterMode: EncounterMode;
  encounterDelaySec: number;
  onChange: (patch: RevealPatch) => void;
};

export function RevealDelaySetting({
  encounterMode,
  encounterDelaySec,
  onChange,
}: RevealDelaySettingProps) {
  const selectMode = (mode: EncounterMode) => {
    if (mode === "delayed") {
      // 現在値がプリセット外なら、backend 既定 (3600 = 1 時間) に寄せて
      // 必ずどれかのプリセットが選ばれた状態にする。
      onChange(
        isPreset(encounterDelaySec)
          ? { encounterMode: "delayed" }
          : {
              encounterMode: "delayed",
              encounterDelaySec: ENCOUNTER_DELAY_PRESETS[0],
            },
      );
      return;
    }
    onChange({ encounterMode: mode });
  };

  return (
    <div className={styles.card}>
      <div className={styles.head}>
        <span className={styles.label}>相手に見えるまで</span>
        <span className={styles.value} role="status">
          {valueText(encounterMode, encounterDelaySec)}
        </span>
      </div>

      <div className={styles.modes} role="group" aria-label="相手に見えるまで">
        {MODES.map((mode) => {
          const selected = mode.value === encounterMode;
          return (
            <Pressable
              key={mode.value}
              type="button"
              role="switch"
              aria-checked={selected}
              className={[styles.mode, selected && styles.modeSelected]
                .filter(Boolean)
                .join(" ")}
              onClick={() => selectMode(mode.value)}
            >
              {mode.label}
            </Pressable>
          );
        })}
      </div>

      {encounterMode === "delayed" && (
        <div className={styles.presets} role="group" aria-label="公開までの時間">
          <span className={styles.presetsLabel}>公開までの時間</span>
          <SegmentedControl
            options={ENCOUNTER_DELAY_PRESETS.map((sec) => ({
              value: String(sec),
              label: PRESET_LABELS[sec],
            }))}
            value={String(encounterDelaySec)}
            onChange={(next) => onChange({ encounterDelaySec: Number(next) })}
          />
        </div>
      )}

      <Notice tone="info" icon="info">
        相手と設定がちがうときは、長いほうが使われます
      </Notice>
    </div>
  );
}
