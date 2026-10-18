"use client";

import type { ReactNode } from "react";
import { useRouter } from "next/navigation";
import type { FeedPost, FeedSpace } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { relativeTime } from "@/data/format";
import { useNow } from "@/hooks/useNow";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Fab } from "@/components/ui/Fab";
import { Icon } from "@/components/ui/Icon";
import { ImagePlaceholder } from "@/components/ui/ImagePlaceholder";
import { PressableLink } from "@/components/ui/Pressable";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import styles from "./HomeFeedScreen.module.css";

/**
 * フィードの取得元 (17 §3.1)。ホームは公開投稿、近くは今この場にいる人 (近接) の投稿、
 * グループは参加中グループの投稿。
 */
export type FeedSource = "home" | "nearby" | "groups";

const SOURCES: { value: FeedSource; label: string }[] = [
  { value: "home", label: "ホーム" },
  { value: "nearby", label: "近く" },
  { value: "groups", label: "グループ" },
];

const SUBTITLES: Record<FeedSource, string> = {
  home: "フレンドやすれ違った人の公開投稿",
  nearby: "今この場にいる人の投稿",
  groups: "参加中のグループの投稿",
};

const EMPTY_DESCRIPTIONS: Record<FeedSource, string> = {
  home: "フレンドやすれ違った人の公開投稿がここに並びます。",
  nearby: "今この場にいる人の投稿がここに並びます。",
  groups: "参加中のグループに投稿されると、ここに並びます。",
};

export type HomeFeedScreenProps = {
  source: FeedSource;
  onSourceChange: (source: FeedSource) => void;
  /** GET /feed?source={source} (17 §3.1) の取得状態。 */
  state: ResourceState<FeedPost[]>;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
  onRetry?: () => void;
};

function spaceHref(space: FeedSpace): string {
  return space.type === "party"
    ? "/party/"
    : `/group/?id=${encodeURIComponent(space.id)}`;
}

export function HomeFeedScreen({
  source,
  onSourceChange,
  state,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  onRetry,
}: HomeFeedScreenProps) {
  const router = useRouter();
  const now = useNow(60_000);
  const posts = state.status === "ok" ? state.data : [];

  // ホームからは公開だけ、グループからは既定の Space を送信先にして Compose を開く。
  const goCompose = () => {
    router.push(source === "home" ? "/compose/?target=public" : "/compose/");
  };

  return (
    <AppShell tab="feed">
      <ScreenHeader title="フィード" subtitle={SUBTITLES[source]} />

      <div className={styles.gutter}>
        <SegmentedControl options={SOURCES} value={source} onChange={onSourceChange} />
      </div>

      {state.status === "ok" ? (
        posts.length > 0 ? (
          <section className={styles.section}>
            <ul className={styles.list}>
              {posts.map((post) => (
                <li key={post.uri}>
                  <Card padding="md" className={styles.item}>
                    {post.space && (
                      <PressableLink className={styles.space} href={spaceHref(post.space)}>
                        <Icon
                          name={post.space.type === "party" ? "celebration" : "hub"}
                          size={14}
                        />
                        <span className={styles.spaceName}>{post.space.name}</span>
                      </PressableLink>
                    )}
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
            description={EMPTY_DESCRIPTIONS[source]}
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
          title="フィードは準備中です"
          description="この機能はまだ提供されていません。"
        />
      )}

      <Fab label="投稿する" onClick={goCompose} />
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
