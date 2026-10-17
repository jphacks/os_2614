"use client";

import { useCallback, useState, type ReactNode } from "react";
import type { Friend, FriendLocation, LocationPrecision } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { FriendRow } from "@/components/feature/friend";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { List } from "@/components/ui/List";
import { SectionHeader } from "@/components/ui/SectionHeader";
import styles from "./FriendsScreen.module.css";
import { PressableLink } from "@/components/ui/Pressable";

const EMPTY_TITLE = "まだ友達はいません";
const EMPTY_DESCRIPTION = "すれ違ったことのある人とだけ友達になれます。";

export type FriendsScreenProps = {
  /**
   * GET /friends (07 §2.3) の取得状態。ok のときだけ data (マップ済み Friend[]) を
   * 描画し、loading / error / empty / unavailable は画面を空にせず出し分ける。
   */
  state: ResourceState<Friend[]>;
  /** 次のページがまだあるか (usePagedResource)。 */
  hasMore?: boolean;
  /** 次のページを取得中か。 */
  loadingMore?: boolean;
  /** 次のページを末尾に足す。 */
  onLoadMore?: () => void;
  /** DELETE /friends/{did} (07 §2.3)。成功しなければ reject する。 */
  onDelete: (did: string) => Promise<void>;
  /** GET /friends/locations (07 §4.2) の結果。did で引く。 */
  locations?: ReadonlyMap<string, FriendLocation>;
  /** GET /friends/locations が ok のときだけ true。未取得の行は「位置は準備中」。 */
  locationsReady?: boolean;
  /** PUT /friends/{did}/location-share (07 §4.2)。成功しなければ reject する。 */
  onSharePrecision?: (
    did: string,
    precision: LocationPrecision,
    areasOnly: boolean,
  ) => Promise<void>;
  /** error 状態の再取得。useResource / usePagedResource の reload を渡す。 */
  onRetry?: () => void;
};

export function FriendsScreen({
  state,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  onDelete,
  locations,
  locationsReady = false,
  onSharePrecision,
  onRetry,
}: FriendsScreenProps) {
  // 楽観削除のための画面ローカル状態。送信中は行を隠し、失敗したら戻す。
  const [removing, setRemoving] = useState<ReadonlySet<string>>(new Set());
  const [failedName, setFailedName] = useState<string | null>(null);

  const handleDelete = useCallback(
    async (friend: Friend) => {
      setFailedName(null);
      setRemoving((prev) => new Set(prev).add(friend.did));
      try {
        await onDelete(friend.did);
      } catch {
        // 失敗したら消した行を戻し、インラインで知らせる。
        setRemoving((prev) => {
          const next = new Set(prev);
          next.delete(friend.did);
          return next;
        });
        setFailedName(friend.name);
      }
    },
    [onDelete],
  );

  const friends =
    state.status === "ok" ? state.data.filter((friend) => !removing.has(friend.did)) : [];

  return (
    <AppShell tab="friends">
      <ScreenHeader
        title="友達"
        count={state.status === "ok" ? `${friends.length}人` : undefined}
        subtitle="位置はエリア名だけ共有しています"
        trailing={
          <PressableLink
            className={styles.addFriend}
            href="/friends/requests/"
            aria-label="友達申請と招待"
          >
            <Icon name="person_add" size={22} />
          </PressableLink>
        }
      />

      {state.status === "ok" ? (
        <>
          {failedName && (
            <p className={styles.deleteError} role="alert">
              {failedName}さんを削除できませんでした。もう一度お試しください。
            </p>
          )}

          {friends.length > 0 ? (
            <section className={styles.section}>
              <SectionHeader title="みんな" />
              <Card padding="md">
                <List>
                  {friends.map((friend) => (
                    <FriendRow
                      key={friend.did}
                      friend={friend}
                      location={locationsReady ? (locations?.get(friend.did) ?? null) : null}
                      onDelete={handleDelete}
                      onShare={
                        onSharePrecision
                          ? (precision, areasOnly) =>
                              onSharePrecision(friend.did, precision, areasOnly)
                          : undefined
                      }
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
            <FriendsState
              icon="group_off"
              title={EMPTY_TITLE}
              description={EMPTY_DESCRIPTION}
            />
          )}
        </>
      ) : state.status === "loading" ? (
        <FriendsSkeleton />
      ) : state.status === "error" ? (
        <FriendsState
          icon="cloud_off"
          title="友達を読み込めませんでした"
          description="通信の状態を確認して、もう一度お試しください。"
          action={
            onRetry ? (
              <Button variant="secondary" icon="refresh" onClick={onRetry}>
                再読み込み
              </Button>
            ) : undefined
          }
        />
      ) : state.status === "empty" ? (
        <FriendsState
          icon="group_off"
          title={EMPTY_TITLE}
          description={EMPTY_DESCRIPTION}
        />
      ) : (
        // unavailable (404・405・501) は「まだ無い / 未対応」。
        <FriendsState
          icon="construction"
          title="友達リストは準備中です"
          description="この機能はまだ提供されていません。"
        />
      )}

      <p className={styles.footer}>
        検索はありません。すれ違ったことのある人とだけ友達になれます
      </p>
    </AppShell>
  );
}

type FriendsStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function FriendsState({ icon, title, description, action }: FriendsStateProps) {
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

function FriendsSkeleton() {
  return (
    <div className={styles.skeleton} aria-hidden>
      {[0, 1, 2].map((row) => (
        <div key={row} className={styles.skeletonRow}>
          <span className={styles.skeletonAvatar} />
          <div className={styles.skeletonLines}>
            <span className={styles.skeletonLineShort} />
            <span className={styles.skeletonLineLong} />
          </div>
        </div>
      ))}
    </div>
  );
}
