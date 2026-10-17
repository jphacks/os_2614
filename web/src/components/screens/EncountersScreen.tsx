"use client";

import type { ReactNode } from "react";
import type { Encounter, EncounterRelation } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { clockTime, dateRange } from "@/data/format";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { List, ListItem } from "@/components/ui/List";
import { Tag } from "@/components/ui/Tag";
import styles from "./EncountersScreen.module.css";

const RELATION_LABELS: Record<EncounterRelation, string> = {
  friend: "友達",
  requested: "申請済み",
  incoming: "承認待ち",
  none: "会った人",
};

export type EncountersScreenProps = {
  /** GET /encounters (06 §6) の取得状態。ok のときだけ data を描画する。 */
  state: ResourceState<Encounter[]>;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
  onRetry?: () => void;
};

export function EncountersScreen({
  state,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  onRetry,
}: EncountersScreenProps) {
  const encounters = state.status === "ok" ? state.data : [];

  return (
    <AppShell>
      <ScreenHeader
        back="/"
        title="すれ違い"
        count={state.status === "ok" ? `${encounters.length}件` : undefined}
        subtitle="すれ違った人だけが表示されます"
      />

      {state.status === "ok" ? (
        <>
          {encounters.length > 0 ? (
            <section className={styles.section}>
              <Card padding="md">
                <List>
                  {encounters.map((encounter) => (
                    <ListItem
                      key={encounter.id}
                      href={`/users/?did=${encodeURIComponent(encounter.peer.did)}&encounter=${encodeURIComponent(encounter.id)}&relation=${encounter.relation}`}
                      leading={<Avatar user={encounter.peer} size={40} />}
                      title={encounter.peer.name}
                      subtitle={`${dateRange(encounter.seenFrom, encounter.seenUntil)}・${clockTime(encounter.seenFrom)}〜${clockTime(encounter.seenUntil)}・${encounter.timesWithPeer}回`}
                      trailing={<Tag label={RELATION_LABELS[encounter.relation]} />}
                    />
                  ))}
                </List>
              </Card>
              {hasMore && (
                <div className={styles.more}>
                  <Button
                    variant="secondary"
                    onClick={onLoadMore}
                    disabled={loadingMore}
                  >
                    {loadingMore ? "読み込み中…" : "もっと見る"}
                  </Button>
                </div>
              )}
            </section>
          ) : (
            <EncountersState
              icon="near_me"
              title="まだすれ違いはありません"
              description="近くの人とすれ違うと、ここに残ります。"
            />
          )}
        </>
      ) : state.status === "loading" ? (
        <EncountersSkeleton />
      ) : state.status === "error" ? (
        <EncountersState
          icon="cloud_off"
          title="すれ違いを読み込めませんでした"
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
        <EncountersState
          icon="construction"
          title="すれ違いは準備中です"
          description="この機能はまだ提供されていません。"
        />
      )}
    </AppShell>
  );
}

type EncountersStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function EncountersState({ icon, title, description, action }: EncountersStateProps) {
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

function EncountersSkeleton() {
  return (
    <div className={styles.skeleton} aria-hidden>
      {[0, 1, 2].map((row) => (
        <div key={row} className={styles.skeletonRow} />
      ))}
    </div>
  );
}
