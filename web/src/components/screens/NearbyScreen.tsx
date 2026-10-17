"use client";

import { useCallback, useState, type ReactNode } from "react";
import type { GroupNearby } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import type { ProximityStatus } from "@/device/proximity/definitions";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { List, ListItem } from "@/components/ui/List";
import { Notice } from "@/components/ui/Notice";
import { SectionHeader } from "@/components/ui/SectionHeader";
import { Tag } from "@/components/ui/Tag";
import styles from "./NearbyScreen.module.css";
import { PressableLink } from "@/components/ui/Pressable";

export type NearbyScreenProps = {
  /** GET /groups/nearby (09 §5) の取得状態。ok のときだけ data を描画する。 */
  state: ResourceState<GroupNearby>;
  /** すれ違いモードの現在状態 (useProximityStatus)。ブラウザでは null。 */
  proximity: ProximityStatus | null;
  /** POST /groups/{id}/join (09 §5)。成功しなければ reject する。 */
  onJoin: (id: string) => Promise<void>;
  /** POST /groups/{id}/leave (09 §5)。成功しなければ reject する。 */
  onLeave: (id: string) => Promise<void>;
  /** error 状態の再取得。 */
  onRetry?: () => void;
  /** 403 location_stale のときの位置要求 (権限要求 → 再取得)。 */
  onEnableLocation?: () => void;
};

export function NearbyScreen({
  state,
  proximity,
  onJoin,
  onLeave,
  onRetry,
  onEnableLocation,
}: NearbyScreenProps) {
  const [pending, setPending] = useState<Record<string, "join" | "leave">>({});
  const [actionError, setActionError] = useState<string | null>(null);

  const run = useCallback(
    async (id: string, kind: "join" | "leave") => {
      setActionError(null);
      setPending((prev) => ({ ...prev, [id]: kind }));
      try {
        if (kind === "join") {
          await onJoin(id);
        } else {
          await onLeave(id);
        }
      } catch {
        setActionError("グループを更新できませんでした。もう一度お試しください。");
      } finally {
        setPending((prev) => {
          const next = { ...prev };
          delete next[id];
          return next;
        });
      }
    },
    [onJoin, onLeave],
  );

  const locationStale =
    state.status === "error" && state.error.code === "location_stale";

  const inside = state.status === "ok" ? state.data.inside : [];
  const near = state.status === "ok" ? state.data.near : [];

  return (
    <AppShell tab="nearby">
      <ScreenHeader
        title="ちかく"
        subtitle="近くのグループ"
        trailing={
          <span className={styles.headerActions}>
            <PressableLink className={styles.headerLink} href="/home/" aria-label="ホーム">
              <Icon name="public" size={22} />
            </PressableLink>
            <PressableLink className={styles.headerLink} href="/notifications/" aria-label="通知">
              <Icon name="notifications" size={22} />
            </PressableLink>
            <PressableLink className={styles.headerLink} href="/encounters/" aria-label="すれ違い">
              <Icon name="near_me" size={22} />
            </PressableLink>
            <PressableLink
              className={styles.headerLink}
              href="/group/graph/"
              aria-label="グループの関係"
            >
              <Icon name="account_tree" size={22} />
            </PressableLink>
          </span>
        }
      />

      {locationStale && (
        <div className={styles.banner}>
          <Notice tone="warning" icon="location_on">
            グループを見るには位置情報が必要です
          </Notice>
          {onEnableLocation && (
            <Button variant="secondary" size="sm" onClick={onEnableLocation}>
              位置を有効にする
            </Button>
          )}
        </div>
      )}

      {actionError && (
        <p className={styles.actionError} role="alert">
          {actionError}
        </p>
      )}

      {state.status === "ok" ? (
        <>
          {inside.length > 0 && (
            <section className={styles.section}>
              <SectionHeader title="いまいるエリア" />
              <Card padding="md">
                <List>
                  {inside.map((group) => (
                    <ListItem
                      key={group.id}
                      title={group.name}
                      subtitle={`メンバー ${group.memberCount}人`}
                      trailing={
                        <Button
                          variant={group.isMember ? "secondary" : "primary"}
                          size="sm"
                          disabled={pending[group.id] !== undefined}
                          onClick={() =>
                            void run(group.id, group.isMember ? "leave" : "join")
                          }
                        >
                          {pending[group.id] === "join"
                            ? "参加中…"
                            : pending[group.id] === "leave"
                              ? "退出中…"
                              : group.isMember
                                ? "退出"
                                : "参加"}
                        </Button>
                      }
                    />
                  ))}
                </List>
              </Card>
            </section>
          )}

          {near.length > 0 && (
            <section className={styles.section}>
              <SectionHeader title="近くのグループ" />
              <Card padding="md">
                <List>
                  {near.map((group) => (
                    <ListItem
                      key={group.id}
                      title={group.name}
                      subtitle={`メンバー ${group.memberCount}人`}
                      trailing={group.distanceBucket ? <Tag label={group.distanceBucket} /> : undefined}
                    />
                  ))}
                </List>
              </Card>
            </section>
          )}

          {inside.length === 0 && near.length === 0 && (
            <NearbyState
              icon="hub"
              title="近くのグループはありません"
              description="エリアに入ると、参加できるグループがここに並びます。"
            />
          )}
        </>
      ) : state.status === "loading" ? (
        <NearbySkeleton />
      ) : state.status === "error" ? (
        locationStale ? (
          <NearbyState
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
          <NearbyState
            icon="cloud_off"
            title="近くのグループを読み込めませんでした"
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
      ) : (
        <NearbyState
          icon="construction"
          title="ちかくは準備中です"
          description="この機能はまだ提供されていません。"
        />
      )}

      <div className={styles.footer}>
        <Notice tone="info" icon="info">
          {proximity?.running
            ? "すれ違い機能が動いています"
            : "すれ違いには位置情報と Bluetooth の許可が必要です"}
        </Notice>
      </div>
    </AppShell>
  );
}

type NearbyStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function NearbyState({ icon, title, description, action }: NearbyStateProps) {
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

function NearbySkeleton() {
  return (
    <div className={styles.skeleton} aria-hidden>
      {[0, 1, 2].map((row) => (
        <div key={row} className={styles.skeletonRow} />
      ))}
    </div>
  );
}
