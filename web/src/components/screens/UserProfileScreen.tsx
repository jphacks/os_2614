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
import { StatusBadge } from "@/components/ui/StatusBadge";
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
  /** Bluesky でフォローする (POST /users/{did}/follow)。 */
  onFollow?: () => Promise<FriendRequestResult>;
  /** Bluesky のフォローを外す (DELETE /users/{did}/follow)。相互ならフレンドも外れる。 */
  onUnfollow?: () => Promise<FriendRequestResult>;
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
  onFollow,
  onUnfollow,
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
            onFollow={onFollow}
            onUnfollow={onUnfollow}
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
  onFollow?: () => Promise<FriendRequestResult>;
  onUnfollow?: () => Promise<FriendRequestResult>;
};

function ProfileBody({
  user,
  did,
  isSelf,
  relation,
  onRequestFriend,
  onFollow,
  onUnfollow,
}: ProfileBodyProps) {
  const [sending, setSending] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  // フォロー / 解除が成功したら、再取得を待たずに表示を切り替える。
  const [followingOverride, setFollowingOverride] = useState<boolean | null>(
    null,
  );
  // GET /users/{did} が following を返さない (古い backend) ときは undefined のまま、
  // すれ違いから渡された relation の表示に戻す。
  const following = followingOverride ?? user.following;
  const mutual = following === true && user.followedBy === true;

  const run = async (
    action: () => Promise<FriendRequestResult>,
    unavailable: string,
  ): Promise<boolean> => {
    if (sending) return false;
    setSending(true);
    setMessage(null);
    const result = await action();
    setSending(false);
    if (result.status === "unavailable") {
      setMessage(unavailable);
    } else if (result.status === "error") {
      setMessage(result.message);
    }
    return result.status === "ok";
  };

  const send = async () => {
    if (!onRequestFriend) return;
    if (await run(onRequestFriend, "友達申請はまだ使えません")) {
      setFollowingOverride(true);
    }
  };

  const follow = async () => {
    if (!onFollow) return;
    if (await run(onFollow, "フォローはまだ使えません")) {
      setFollowingOverride(true);
    }
  };

  const unfollow = async () => {
    if (!onUnfollow) return;
    if (
      mutual &&
      !window.confirm("フォローを外すと、フレンドも解除されます。よろしいですか？")
    ) {
      return;
    }
    if (await run(onUnfollow, "フォロー解除はまだ使えません")) {
      setFollowingOverride(false);
    }
  };

  // 友達申請 (すれ違いの裏付けつきフォロー) を出せるときは、そちらを優先する。
  const canRequestFriend =
    !isSelf && following !== true && relation === "none" && !!onRequestFriend;

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
          <h1 className={styles.name}>{user.name}</h1>
          <StatusBadge status={user.status ?? ""} />
          {user.handle && <p className={styles.handle}>@{user.handle}</p>}
        </div>
        {!isSelf && following !== undefined && !canRequestFriend && (
          <div className={styles.friendAction}>
            {mutual && <Tag label="友達" />}
            {following ? (
              <Button
                variant="secondary"
                size="sm"
                icon="person_remove"
                onClick={unfollow}
                disabled={sending || !onUnfollow}
              >
                {sending ? "解除中…" : "フォロー解除"}
              </Button>
            ) : (
              <Button
                variant="primary"
                size="sm"
                icon="person_add"
                onClick={follow}
                disabled={sending || !onFollow}
              >
                {sending ? "送信中…" : "フォロー"}
              </Button>
            )}
          </div>
        )}
        {canRequestFriend && (
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
        {!isSelf && following === undefined && relation === "requested" && (
          <div className={styles.friendAction}>
            <Button variant="secondary" size="sm" icon="schedule" disabled>
              申請済み
            </Button>
          </div>
        )}
        {!isSelf && following === undefined && relation === "friend" && (
          <div className={styles.friendAction}>
            <Tag label="友達" />
          </div>
        )}
      </div>

      {canRequestFriend && (
        <p className={styles.friendActionMessage}>
          申請すると、Bluesky でもフォローします（公開されます）。
        </p>
      )}

      {!isSelf && following === false && !canRequestFriend && (
        <p className={styles.friendActionMessage}>
          {user.followedBy
            ? "フォローされています。フォローを返すとフレンドになります（Bluesky に公開されます）。"
            : "Bluesky でフォローします（公開されます）。"}
        </p>
      )}

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
