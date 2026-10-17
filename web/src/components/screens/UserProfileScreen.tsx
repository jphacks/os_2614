"use client";

import { useState, type ReactNode } from "react";
import type { EncounterRelation, Profile } from "@/domain/types";
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

/** 友達申請 (POST /friends/requests、07 §2.2) の結果。unavailable はエンドポイント未対応。 */
export type FriendRequestResult =
  | { status: "ok" }
  | { status: "unavailable" }
  | { status: "error"; message: string };

export type UserProfileScreenProps = {
  /** クエリ `?did=` の対象 DID。公開投稿の取得にも使う。 */
  did: string;
  /** GET /users/{did} (13 §1) の取得状態。ok のときだけ data を描画する。 */
  state: ResourceState<Profile>;
  /** GET /me の DID が対象と一致したときだけ true。判定できなければ false。 */
  isSelf?: boolean;
  /** すれ違い一覧から渡された相手との関係。無ければ申請 UI を出さない。 */
  relation?: EncounterRelation;
  /** 友達申請を送る。すれ違いの ID が分かるときだけ渡す。 */
  onRequestFriend?: () => Promise<FriendRequestResult>;
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
  relation,
  onRequestFriend,
  onRetry,
}: UserProfileScreenProps) {
  return (
    <AppShell tab="friends">
      <ScreenHeader title="プロフィール" back />
      <div className={styles.content}>
        {state.status === "ok" ? (
          <ProfileBody
            user={state.data}
            did={did}
            isSelf={isSelf}
            relation={relation}
            onRequestFriend={onRequestFriend}
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
  relation?: EncounterRelation;
  onRequestFriend?: () => Promise<FriendRequestResult>;
};

function ProfileBody({
  user,
  did,
  isSelf,
  relation,
  onRequestFriend,
}: ProfileBodyProps) {
  const [sending, setSending] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  const send = async () => {
    if (!onRequestFriend || sending) return;
    setSending(true);
    setMessage(null);
    const result = await onRequestFriend();
    setSending(false);
    if (result.status === "unavailable") {
      setMessage("友達申請はまだ使えません");
    } else if (result.status === "error") {
      setMessage(result.message);
    }
  };

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

      <div className={styles.identityRow}>
        <div className={styles.identity}>
          <div className={styles.nameRow}>
            <h1 className={styles.name}>{user.name}</h1>
            {user.status && <Tag label={user.status} />}
          </div>
          {user.handle && <p className={styles.handle}>@{user.handle}</p>}
        </div>
        {!isSelf && relation === "none" && onRequestFriend && (
          <div className={styles.friendAction}>
            <Button
              variant="primary"
              size="sm"
              icon="person_add"
              onClick={send}
              disabled={sending}
            >
              {sending ? "送信中…" : "友達申請"}
            </Button>
          </div>
        )}
        {!isSelf && relation === "requested" && (
          <div className={styles.friendAction}>
            <Button variant="secondary" size="sm" icon="schedule" disabled>
              申請済み
            </Button>
          </div>
        )}
        {!isSelf && relation === "friend" && (
          <div className={styles.friendAction}>
            <Tag label="友達" />
          </div>
        )}
      </div>

      {!isSelf && message && (
        <p className={styles.friendActionMessage} role="alert">
          {message}
        </p>
      )}

      {!isSelf && relation === "incoming" && (
        <Card padding="md">
          <List>
            <ListItem
              leading={<Icon name="mail" size={22} />}
              title="この人から友達申請が届いています"
              href="/friends/requests/"
              trailing={<Icon name="chevron_right" size={20} />}
            />
          </List>
        </Card>
      )}

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
