"use client";

import type { ReactNode } from "react";
import type { Profile } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { PublicPostsSection } from "@/components/feature/post";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { List, ListItem } from "@/components/ui/List";
import { Tag } from "@/components/ui/Tag";
import styles from "./UserProfileScreen.module.css";

export type UserProfileScreenProps = {
  /** クエリ `?did=` の対象 DID。公開投稿の取得にも使う。 */
  did: string;
  /** GET /users/{did} (13 §1) の取得状態。ok のときだけ data を描画する。 */
  state: ResourceState<Profile>;
  /** GET /me の DID が対象と一致したときだけ true。判定できなければ false。 */
  isSelf?: boolean;
  /** error 状態の再取得。useResource の reload を渡す。 */
  onRetry?: () => void;
};

/**
 * ほかのユーザーのプロフィール。GET /users/{did} は統計 (counts) を返さないため、
 * 数字は出さず、名前・ハンドル・bio・ステータス・バナー・公開投稿だけを見せる。
 */
export function UserProfileScreen({
  did,
  state,
  isSelf = false,
  onRetry,
}: UserProfileScreenProps) {
  return (
    <AppShell tab="friends">
      <ScreenHeader title="プロフィール" back />
      <div className={styles.content}>
        {state.status === "ok" ? (
          <ProfileBody user={state.data} did={did} isSelf={isSelf} />
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
            description="このユーザーのプロフィールはまだ表示できません。"
          />
        )}
      </div>
    </AppShell>
  );
}

type ProfileBodyProps = {
  user: Profile;
  did: string;
  isSelf: boolean;
};

function ProfileBody({ user, did, isSelf }: ProfileBodyProps) {
  return (
    <>
      <div className={styles.bannerWrap}>
        <div className={styles.banner}>
          {user.bannerUrl ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img className={styles.bannerImage} src={user.bannerUrl} alt="" />
          ) : (
            <>
              <span className={`${styles.cloud} ${styles.cloudA}`} aria-hidden />
              <span className={`${styles.cloud} ${styles.cloudB}`} aria-hidden />
              <span className={`${styles.cloud} ${styles.cloudC}`} aria-hidden />
            </>
          )}
        </div>
        <span className={styles.avatar}>
          <Avatar user={user} size={80} />
        </span>
      </div>

      <div className={styles.identity}>
        <div className={styles.nameRow}>
          <h1 className={styles.name}>{user.name}</h1>
          {user.status && <Tag label={user.status} />}
        </div>
        {user.handle && <p className={styles.handle}>@{user.handle}</p>}
      </div>

      {user.bio && <p className={styles.bio}>{user.bio}</p>}

      {isSelf && (
        <Card padding="md">
          <List>
            <ListItem
              leading={<Icon name="person" size={22} />}
              title="自分のプロフィール"
              href="/profile/"
              trailing={<Icon name="chevron_right" size={20} />}
            />
          </List>
        </Card>
      )}

      <PublicPostsSection did={did} />
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
