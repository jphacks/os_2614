"use client";

import { useCallback, useState, type ReactNode } from "react";
import type { Notification, NotificationType } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { clockTime } from "@/data/format";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { List, ListItem } from "@/components/ui/List";
import styles from "./NotificationsScreen.module.css";

const TYPE_LABELS: Record<NotificationType, string> = {
  "friend.request": "フレンド申請が届きました",
  "friend.accepted": "フレンド申請が承認されました",
  "party.confirmed": "パーティが確定しました",
  "group.nearby": "近くにグループがあります",
  "group.invited": "グループに招待されました",
  "group.suggested": "グループを提案されました",
  "encounter.daily": "今日のすれ違い",
  "encounter.weekly": "今週のすれ違い",
  "encounter.monthly": "今月のすれ違い",
  "moderation.hidden": "投稿が非表示になりました",
};

const TYPE_ICONS: Record<NotificationType, string> = {
  "friend.request": "person_add",
  "friend.accepted": "how_to_reg",
  "party.confirmed": "celebration",
  "group.nearby": "hub",
  "group.invited": "mail",
  "group.suggested": "lightbulb",
  "encounter.daily": "near_me",
  "encounter.weekly": "near_me",
  "encounter.monthly": "near_me",
  "moderation.hidden": "visibility_off",
};

export type NotificationsScreenProps = {
  /** GET /notifications (12 §1) の取得状態。 */
  state: ResourceState<Notification[]>;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
  onRetry?: () => void;
  /** 1 件を既読にする (POST /notifications/read {ids})。 */
  onMarkRead: (ids: string[]) => Promise<void>;
  /** すべて既読にする (POST /notifications/read {all:true})。 */
  onMarkAllRead: () => Promise<void>;
};

export function NotificationsScreen({
  state,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  onRetry,
  onMarkRead,
  onMarkAllRead,
}: NotificationsScreenProps) {
  const [readIds, setReadIds] = useState<ReadonlySet<string>>(new Set());
  const [error, setError] = useState<string | null>(null);

  const markRead = useCallback(
    async (id: string) => {
      setReadIds((prev) => new Set(prev).add(id));
      try {
        await onMarkRead([id]);
      } catch {
        setReadIds((prev) => {
          const next = new Set(prev);
          next.delete(id);
          return next;
        });
        setError("既読にできませんでした。");
      }
    },
    [onMarkRead],
  );

  const markAll = useCallback(async () => {
    setError(null);
    try {
      await onMarkAllRead();
    } catch {
      setError("既読にできませんでした。");
    }
  }, [onMarkAllRead]);

  const notifications = state.status === "ok" ? state.data : [];
  const hasUnread = notifications.some((item) => !item.readAt && !readIds.has(item.id));

  return (
    <AppShell>
      <ScreenHeader
        back
        title="通知"
        trailing={
          hasUnread ? (
            <Button variant="ghost" size="sm" onClick={() => void markAll()}>
              すべて既読
            </Button>
          ) : undefined
        }
      />

      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}

      {state.status === "ok" ? (
        notifications.length > 0 ? (
          <section className={styles.section}>
            <Card padding="md">
              <List>
                {notifications.map((item) => {
                  const unread = !item.readAt && !readIds.has(item.id);
                  return (
                    <ListItem
                      key={item.id}
                      onClick={unread ? () => void markRead(item.id) : undefined}
                      leading={
                        <span className={styles.iconCircle} aria-hidden>
                          <Icon name={TYPE_ICONS[item.type]} size={20} />
                        </span>
                      }
                      title={TYPE_LABELS[item.type]}
                      subtitle={clockTime(item.createdAt)}
                      trailing={
                        unread ? (
                          <span className={styles.unreadDot} aria-label="未読" />
                        ) : undefined
                      }
                    />
                  );
                })}
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
          <NotificationsState
            icon="notifications_none"
            title="通知はありません"
            description="申請やパーティの確定がここに届きます。"
          />
        )
      ) : state.status === "loading" ? (
        <NotificationsSkeleton />
      ) : state.status === "error" ? (
        <NotificationsState
          icon="cloud_off"
          title="通知を読み込めませんでした"
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
        <NotificationsState
          icon="construction"
          title="通知は準備中です"
          description="この機能はまだ提供されていません。"
        />
      )}
    </AppShell>
  );
}

type NotificationsStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function NotificationsState({ icon, title, description, action }: NotificationsStateProps) {
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

function NotificationsSkeleton() {
  return (
    <div className={styles.skeleton} aria-hidden>
      {[0, 1, 2].map((row) => (
        <div key={row} className={styles.skeletonRow} />
      ))}
    </div>
  );
}
