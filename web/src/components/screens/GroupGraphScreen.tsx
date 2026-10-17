"use client";

import type { ReactNode } from "react";
import type {
  GroupGraph,
  GroupGraphEdge,
  GroupGraphEdgeKind,
} from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { List, ListItem } from "@/components/ui/List";
import { Notice } from "@/components/ui/Notice";
import { SectionHeader } from "@/components/ui/SectionHeader";
import { Tag } from "@/components/ui/Tag";
import styles from "./GroupGraphScreen.module.css";

export type GroupGraphScreenProps = {
  /** GET /groups/graph (09 §7) の取得状態。中心座標は含まない。 */
  state: ResourceState<GroupGraph>;
  /** error 状態の再取得。 */
  onRetry?: () => void;
  /** 403 location_stale のときの位置要求。 */
  onEnableLocation?: () => void;
};

/** 辺の種類の表示 (09 §7)。 */
const EDGE_LABEL: Record<GroupGraphEdgeKind, string> = {
  contains: "含む",
  overlaps: "重なる",
  shared_members: "共通メンバー",
};

/** 半径 (m) をノードの見た目の大きさ (px) にする。地図は使わない。 */
function bubbleSize(radiusM: number): number {
  const raw = Math.round(Math.sqrt(Math.max(0, radiusM)) * 2);
  return Math.min(96, Math.max(32, raw));
}

export function GroupGraphScreen({
  state,
  onRetry,
  onEnableLocation,
}: GroupGraphScreenProps) {
  const locationStale =
    state.status === "error" && state.error.code === "location_stale";

  const graph = state.status === "ok" ? state.data : null;
  const names = new Map<string, string>();
  for (const node of graph?.nodes ?? []) {
    names.set(node.id, node.name);
  }

  return (
    <AppShell>
      <ScreenHeader
        title="グループの関係"
        back="/"
        subtitle="近くのグループの含まれ方"
      />

      {locationStale && (
        <div className={styles.banner}>
          <Notice tone="warning" icon="location_on">
            位置情報がないとグループの関係を見られません
          </Notice>
          {onEnableLocation && (
            <Button variant="secondary" size="sm" onClick={onEnableLocation}>
              位置を有効にする
            </Button>
          )}
        </div>
      )}

      {graph ? (
        <>
          <section className={styles.section}>
            <div
              className={styles.canvas}
              role="img"
              aria-label="グループの大きさ"
            >
              {graph.nodes.map((node) => {
                const size = bubbleSize(node.radiusM);
                return (
                  <span
                    key={node.id}
                    className={[styles.bubble, node.isMember && styles.member]
                      .filter(Boolean)
                      .join(" ")}
                    style={{ width: size, height: size }}
                    title={`${node.name}（半径 ${node.radiusM}m）`}
                  >
                    <span className={styles.bubbleName}>{node.name}</span>
                  </span>
                );
              })}
            </div>
          </section>

          <section className={styles.section}>
            <SectionHeader title="グループ" />
            <Card padding="md">
              <List>
                {graph.nodes.map((node) => (
                  <ListItem
                    key={node.id}
                    title={node.name}
                    subtitle={`半径 ${node.radiusM}m・メンバー ${node.memberCount}人`}
                    trailing={
                      <span className={styles.tags}>
                        {node.isMember && <Tag label="メンバー" />}
                        {node.inside && <Tag label="いまここ" />}
                        {node.origin === "natural" && <Tag label="自然発生" />}
                      </span>
                    }
                  />
                ))}
              </List>
            </Card>
          </section>

          <section className={styles.section}>
            <SectionHeader title="つながり" />
            {graph.edges.length > 0 ? (
              <Card padding="md">
                <List>
                  {graph.edges.map((edge, index) => (
                    <EdgeRow
                      key={`${edge.from}-${edge.to}-${edge.kind}-${index}`}
                      edge={edge}
                      names={names}
                    />
                  ))}
                </List>
              </Card>
            ) : (
              <Card padding="md">
                <p className={styles.empty}>
                  グループ同士の重なりはありません
                </p>
              </Card>
            )}
          </section>
        </>
      ) : state.status === "loading" ? (
        <GraphState
          icon="hub"
          title="読み込み中…"
          description="グループの関係を取得しています。"
        />
      ) : state.status === "error" ? (
        locationStale ? (
          <GraphState
            icon="location_off"
            title="位置情報が古いようです"
            description="位置を有効にして、もう一度お試しください。"
            action={
              onRetry ? (
                <Button variant="secondary" icon="refresh" onClick={onRetry}>
                  再読み込み
                </Button>
              ) : undefined
            }
          />
        ) : (
          <GraphState
            icon="cloud_off"
            title="グループの関係を読み込めませんでした"
            description="通信の状態を確認して、もう一度お試しください。"
            action={
              onRetry ? (
                <Button variant="secondary" icon="refresh" onClick={onRetry}>
                  再読み込み
                </Button>
              ) : undefined
            }
          />
        )
      ) : state.status === "empty" ? (
        <GraphState
          icon="hub"
          title="近くにグループがありません"
          description="エリアに入ると、グループの関係がここに並びます。"
        />
      ) : (
        <GraphState
          icon="construction"
          title="グループの関係は準備中です"
          description="この機能はまだ提供されていません。"
        />
      )}
    </AppShell>
  );
}

type EdgeRowProps = {
  edge: GroupGraphEdge;
  names: Map<string, string>;
};

function EdgeRow({ edge, names }: EdgeRowProps) {
  const from = names.get(edge.from) ?? edge.from;
  const to = names.get(edge.to) ?? edge.to;
  return (
    <ListItem
      title={`${from} と ${to}`}
      subtitle={EDGE_LABEL[edge.kind]}
      trailing={
        edge.kind === "shared_members" && edge.weight !== undefined ? (
          <Tag label={`${edge.weight}人`} />
        ) : undefined
      }
    />
  );
}

type GraphStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function GraphState({ icon, title, description, action }: GraphStateProps) {
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
