"use client";

import type { ReactNode } from "react";
import type { Memory, Profile } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { MemoryThumbCard } from "@/components/feature/memory";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { IconButton } from "@/components/ui/IconButton";
import { List, ListItem } from "@/components/ui/List";
import { SectionHeader } from "@/components/ui/SectionHeader";
import { StatTriple } from "@/components/ui/StatTriple";
import styles from "./ProfileScreen.module.css";

export type ProfileScreenProps = {
  /**
   * GET /me の取得状態。ok のときだけ data (マップ済みの Profile) を描画し、
   * loading / error / unavailable は画面を空にせずそれぞれの状態を出す。
   */
  state: ResourceState<Profile>;
  /** GET /memories (13 §2) は後続 todo。今は [] で空表示にする。 */
  memories: Memory[];
  /** error 状態の再取得。useResource の reload を渡す。 */
  onRetry?: () => void;
  onEditAvatar?: () => void;
  onEditProfile?: () => void;
};

export function ProfileScreen({
  state,
  memories,
  onRetry,
  onEditAvatar,
  onEditProfile,
}: ProfileScreenProps) {
  return (
    <AppShell tab="profile">
      <div className={styles.content}>
        {state.status === "ok" ? (
          <ProfileBody
            me={state.data}
            memories={memories}
            onEditAvatar={onEditAvatar}
            onEditProfile={onEditProfile}
          />
        ) : state.status === "loading" ? (
          <ProfileSkeleton />
        ) : state.status === "error" ? (
          <ProfileState
            icon="cloud_off"
            title="プロフィールを読み込めませんでした"
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
          // empty / unavailable (404・405・501) は「まだ無い / 未対応」。
          <ProfileState
            icon="construction"
            title="プロフィールは準備中です"
            description="この機能はまだ提供されていません。"
          />
        )}
      </div>
    </AppShell>
  );
}

type ProfileBodyProps = {
  me: Profile;
  memories: Memory[];
  onEditAvatar?: () => void;
  onEditProfile?: () => void;
};

function ProfileBody({
  me,
  memories,
  onEditAvatar,
  onEditProfile,
}: ProfileBodyProps) {
  return (
    <>
      <div className={styles.bannerWrap}>
        <div className={styles.banner} aria-hidden>
          <span className={`${styles.cloud} ${styles.cloudA}`} />
          <span className={`${styles.cloud} ${styles.cloudB}`} />
          <span className={`${styles.cloud} ${styles.cloudC}`} />
        </div>
        <div className={styles.bannerEdit}>
          <IconButton
            icon="edit"
            label="バナーを編集"
            variant="surface"
            onClick={onEditAvatar}
          />
        </div>
        <span className={styles.avatar}>
          <Avatar user={me} size={80} />
        </span>
        <div className={styles.editButton}>
          <Button variant="secondary" size="sm" icon="edit" onClick={onEditProfile}>
            編集
          </Button>
        </div>
      </div>

      <div className={styles.nameRow}>
        <h1 className={styles.name}>{me.name}</h1>
        {me.status && (
          <span className={styles.status}>
            <span className={styles.onlineDot} aria-hidden />
            {me.status}
          </span>
        )}
      </div>
      <p className={styles.bio}>{me.bio}</p>

      <StatTriple
        items={[
          { value: me.counts.encounters, label: "すれ違い" },
          { value: me.counts.friends, label: "友達" },
          { value: me.counts.parties, label: "パーティ" },
        ]}
      />

      <section className={styles.section}>
        <SectionHeader
          title="メモリー"
          action={{ label: "すべて見る ›", href: "/memory/" }}
        />
        <div className={styles.memories}>
          {memories.length > 0 ? (
            memories.map((memory) => (
              <MemoryThumbCard key={memory.spaceId} memory={memory} />
            ))
          ) : (
            <p className={styles.memoryEmpty}>まだメモリーはありません</p>
          )}
        </div>
      </section>

      <Card padding="md">
        <List>
          <ListItem
            leading={<Icon name="tune" size={22} />}
            title="設定"
            href="/settings/"
            trailing={<Icon name="chevron_right" size={20} />}
          />
          <ListItem
            leading={<Icon name="insights" size={22} />}
            title="これまでの統計"
            href="/stats/"
            trailing={<Icon name="chevron_right" size={20} />}
          />
        </List>
      </Card>
    </>
  );
}

type ProfileStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function ProfileState({ icon, title, description, action }: ProfileStateProps) {
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

function ProfileSkeleton() {
  return (
    <div className={styles.skeleton} aria-hidden>
      <div className={styles.skeletonBanner} />
      <div className={styles.skeletonIdentity}>
        <span className={styles.skeletonAvatar} />
        <div className={styles.skeletonLines}>
          <span className={styles.skeletonLineShort} />
          <span className={styles.skeletonLineLong} />
        </div>
      </div>
      <div className={styles.skeletonBlock} />
    </div>
  );
}
