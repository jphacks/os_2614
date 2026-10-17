"use client";

import { useState } from "react";
import type { Stats, StatsRange } from "@/domain/types";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import {
  ActivityGrid,
  EncounterChart,
  MiniStatCard,
} from "@/components/feature/stats";
import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import styles from "./StatsScreen.module.css";

const RANGE_OPTIONS: { value: StatsRange; label: string }[] = [
  { value: "week", label: "週" },
  { value: "month", label: "月" },
  { value: "year", label: "年" },
  { value: "all", label: "すべて" },
];

const DELTA_LABEL: Record<StatsRange, string | null> = {
  week: "先週より",
  month: "先月より",
  year: "去年より",
  all: null,
};

function monthLabelOf(series: { label: string }[]): string | null {
  const last = series.at(-1)?.label;
  const match = last?.match(/^(\d+)\//);
  return match ? `${match[1]}月` : null;
}

export type StatsScreenProps = {
  stats: Stats;
  onRangeChange?: (range: StatsRange) => void;
};

export function StatsScreen({ stats, onRangeChange }: StatsScreenProps) {
  const [range, setRange] = useState<StatsRange>(stats.range);

  const handleRange = (next: StatsRange) => {
    setRange(next);
    onRangeChange?.(next);
  };

  const deltaLabel = DELTA_LABEL[range];
  const monthLabel = monthLabelOf(stats.encounters.series);

  return (
    <AppShell>
      <ScreenHeader back title="これまでの統計" />

      <div className={styles.content}>
        <SegmentedControl
          options={RANGE_OPTIONS}
          value={range}
          onChange={handleRange}
        />

        <Card padding="md" className={styles.encounters}>
          <div className={styles.encounterHead}>
            <h2 className={styles.heading}>すれ違い</h2>
            {deltaLabel && (
              <Badge label={`${deltaLabel} +${stats.encounters.delta}`} tone="positive" />
            )}
          </div>
          <div className={styles.valueRow}>
            <span className={styles.value}>
              {stats.encounters.total.toLocaleString("ja-JP")}
            </span>
            {monthLabel && <span className={styles.valueNote}>・{monthLabel}</span>}
          </div>
          <EncounterChart series={stats.encounters.series} />
        </Card>

        <div className={styles.grid}>
          <MiniStatCard
            label="友達"
            value={`${stats.friends.total}人`}
            note={`今月 +${stats.friends.delta}人`}
          />
          <MiniStatCard
            label="パーティ"
            value={`${stats.parties.total}回`}
            note={`合計 ${stats.parties.totalHours}時間`}
          />
          <MiniStatCard
            label="よくすれ違う時間"
            value={stats.peakHourLabel}
            note={stats.peakHourNote}
          />
          <MiniStatCard
            label="よくいるエリア"
            value={stats.topArea.label}
            note={`すれ違いの ${stats.topArea.ratio}%`}
          />
        </div>

        <ActivityGrid
          activity={stats.activity}
          messagesThisMonth={stats.messagesThisMonth}
        />
      </div>
    </AppShell>
  );
}
