"use client";

import type { ReactNode } from "react";
import type { Stats } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { MiniStatCard } from "@/components/feature/stats";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import styles from "./StatsScreen.module.css";

export type StatsScreenProps = {
  /**
   * GET /me/stats (13 §3) の取得状態。ok のときだけ実データを描画し、
   * loading / error / unavailable (404・405・501) はインラインで出し分ける。
   */
  state: ResourceState<Stats>;
  /** error 状態の再取得。useResource の reload を渡す。 */
  onRetry?: () => void;
};

export function StatsScreen({ state, onRetry }: StatsScreenProps) {
  return (
    <AppShell>
      <ScreenHeader back title="これまでの統計" />

      <div className={styles.content}>
        {state.status === "ok" ? (
          <StatsBody stats={state.data} />
        ) : state.status === "loading" ? (
          <StatsSkeleton />
        ) : state.status === "error" ? (
          <StatsState
            icon="cloud_off"
            title="統計を読み込めませんでした"
            description="通信の状態を確認して、もう一度お試しください。"
            action={
              onRetry ? (
                <Button variant="secondary" icon="refresh" onClick={onRetry}>
                  再読み込み
                </Button>
              ) : undefined
            }
          />
        ) : (
          // empty / unavailable は「まだ無い / 未対応」。統計は backend 未実装の期間があるため準備中を出す。
          <StatsState
            icon="construction"
            title="統計は準備中です"
            description="この機能はまだ提供されていません。"
          />
        )}
      </div>
    </AppShell>
  );
}

function StatsBody({ stats }: { stats: Stats }) {
  const { encounters, friends, parties, groups, posts } = stats;

  return (
    <>
      <Card padding="md" className={styles.encounters}>
        <h2 className={styles.heading}>すれ違い</h2>
        <div className={styles.valueRow}>
          <span className={styles.value}>
            {encounters.total.toLocaleString("ja-JP")}
          </span>
          <span className={styles.valueNote}>合計</span>
        </div>
        <div className={styles.grid}>
          <MiniStatCard label="今日" value={`${encounters.today}回`} note="すれ違い" />
          <MiniStatCard label="今週" value={`${encounters.week}回`} note="すれ違い" />
          <MiniStatCard label="今月" value={`${encounters.month}回`} note="すれ違い" />
          <MiniStatCard
            label="ユニーク"
            value={`${encounters.uniquePeople}人`}
            note="相手の人数"
          />
        </div>
      </Card>

      <div className={styles.grid}>
        <MiniStatCard label="友達" value={`${friends}人`} note="累計" />
        <MiniStatCard label="パーティ" value={`${parties.total}回`} note="累計" />
        <MiniStatCard label="グループ" value={`${groups.member}`} note="参加中" />
        <MiniStatCard label="今月の投稿" value={`${posts.month}件`} note="しました" />
      </div>
    </>
  );
}

type StatsStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function StatsState({ icon, title, description, action }: StatsStateProps) {
  return (
    <div className={styles.state}>
      <span className={styles.stateIcon} aria-hidden>
        <Icon name={icon} size={32} />
      </span>
      <p className={styles.stateTitle}>{title}</p>
      <p className={styles.stateDescription}>{description}</p>
      {action}
    </div>
  );
}

function StatsSkeleton() {
  return (
    <div className={styles.skeleton} aria-hidden>
      <div className={styles.skeletonBlock} />
      <div className={styles.grid}>
        <div className={styles.skeletonCard} />
        <div className={styles.skeletonCard} />
        <div className={styles.skeletonCard} />
        <div className={styles.skeletonCard} />
      </div>
    </div>
  );
}
