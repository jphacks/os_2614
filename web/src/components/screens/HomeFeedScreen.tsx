"use client";

import { useState, type ReactNode } from "react";
import type { FeedPost } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { relativeTime } from "@/data/format";
import { useNow } from "@/hooks/useNow";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { ImagePlaceholder } from "@/components/ui/ImagePlaceholder";
import { Notice } from "@/components/ui/Notice";
import styles from "./HomeFeedScreen.module.css";

/** POST /feed/posts (17 §2.1) の結果。 */
export type PublicPostResult =
  | { status: "ok" }
  | { status: "unavailable" }
  | { status: "error"; message: string };

export type HomeFeedScreenProps = {
  /** GET /feed?source=home (17 §3.1) の取得状態。 */
  state: ResourceState<FeedPost[]>;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
  onRetry?: () => void;
  /** 単独の公開投稿を作る。未指定なら投稿欄を出さない。 */
  onCreatePublicPost?: (text: string) => Promise<PublicPostResult>;
};

export function HomeFeedScreen({
  state,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  onRetry,
  onCreatePublicPost,
}: HomeFeedScreenProps) {
  const now = useNow(60_000);
  const posts = state.status === "ok" ? state.data : [];
  const [draft, setDraft] = useState("");
  const [posting, setPosting] = useState(false);
  const [postError, setPostError] = useState<string | null>(null);

  const submit = async () => {
    if (!onCreatePublicPost || posting) {
      return;
    }
    const text = draft.trim();
    if (text.length === 0) {
      return;
    }
    setPostError(null);
    setPosting(true);
    const result = await onCreatePublicPost(text);
    setPosting(false);
    if (result.status === "ok") {
      setDraft("");
      return;
    }
    setPostError(
      result.status === "unavailable"
        ? "公開投稿はまだ利用できません。"
        : result.message,
    );
  };

  return (
    <AppShell>
      <ScreenHeader back="/" title="ホーム" subtitle="みんなの公開投稿" />

      {onCreatePublicPost && (
        <div className={styles.composer}>
          <textarea
            className={styles.composerInput}
            aria-label="公開投稿の本文"
            placeholder="公開で投稿する…"
            value={draft}
            maxLength={1000}
            disabled={posting}
            onChange={(event) => setDraft(event.target.value)}
          />
          <div className={styles.composerActions}>
            <Button
              variant="primary"
              size="sm"
              onClick={() => void submit()}
              disabled={posting || draft.trim().length === 0}
            >
              {posting ? "投稿中…" : "公開する"}
            </Button>
          </div>
          {postError && (
            <Notice tone="warning" icon="error">
              {postError}
            </Notice>
          )}
        </div>
      )}

      {state.status === "ok" ? (
        posts.length > 0 ? (
          <section className={styles.section}>
            <ul className={styles.list}>
              {posts.map((post) => (
                <li key={post.uri}>
                  <Card padding="md" className={styles.item}>
                    <div className={styles.head}>
                      <Avatar user={post.author} size={32} />
                      <div className={styles.headText}>
                        <span className={styles.name}>{post.author.name}</span>
                        <span className={styles.time}>
                          {now > 0 ? relativeTime(post.createdAt, now) : ""}
                        </span>
                      </div>
                    </div>
                    {post.text && <p className={styles.body}>{post.text}</p>}
                    {post.attachments.length > 0 && (
                      <div className={styles.media}>
                        {post.attachments.map((media) => (
                          <ImagePlaceholder key={media.id} src={media.thumbUrl || undefined} />
                        ))}
                      </div>
                    )}
                  </Card>
                </li>
              ))}
            </ul>
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
          <HomeState
            icon="dynamic_feed"
            title="まだ投稿がありません"
            description="フレンドやすれ違った人の公開投稿がここに並びます。"
          />
        )
      ) : state.status === "loading" ? (
        <HomeSkeleton />
      ) : state.status === "error" ? (
        <HomeState
          icon="cloud_off"
          title="フィードを読み込めませんでした"
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
        <HomeState
          icon="construction"
          title="ホームは準備中です"
          description="この機能はまだ提供されていません。"
        />
      )}
    </AppShell>
  );
}

type HomeStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function HomeState({ icon, title, description, action }: HomeStateProps) {
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

function HomeSkeleton() {
  return (
    <div className={styles.skeleton} aria-hidden>
      {[0, 1, 2].map((row) => (
        <div key={row} className={styles.skeletonRow} />
      ))}
    </div>
  );
}
