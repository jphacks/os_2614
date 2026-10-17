"use client";

import type { FeedItem as FeedItemDTO } from "@/data/dto";
import { relativeTime } from "@/data/format";
import { usePagedResource } from "@/data/hooks";
import { toFeedPost } from "@/data/mappers";
import type { FeedPost } from "@/domain/types";
import { useNow } from "@/hooks/useNow";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { SectionHeader } from "@/components/ui/SectionHeader";
import styles from "./PublicPostsSection.module.css";

const VISIBLE_LIMIT = 5;

export type PublicPostsSectionProps = {
  /** 表示する本人の DID。GET /feed?source=user:{did} で自分の公開投稿を取る (17 §3.2)。 */
  did: string;
};

/**
 * プロフィールに置く「自分の公開投稿」。source=user:{did} の共通フィードを読み、
 * 本文かメディアを持つ投稿だけを新しい順に数件見せる。未対応なら「準備中」を出す。
 */
export function PublicPostsSection({ did }: PublicPostsSectionProps) {
  const posts = usePagedResource<FeedItemDTO>(
    `/feed?source=user:${encodeURIComponent(did)}`,
    [did],
  );
  const now = useNow(60_000);
  const items: FeedPost[] = posts.items
    .map(toFeedPost)
    .filter((post) => post.text.length > 0 || post.attachments.length > 0);

  return (
    <section className={styles.section}>
      <SectionHeader title="公開投稿" />
      <Card padding="md">
        {posts.state.status === "loading" ? (
          <p className={styles.state} role="status">
            読み込んでいます…
          </p>
        ) : posts.state.status === "error" ? (
          <div className={styles.errorBlock} role="alert">
            <p className={styles.state}>公開投稿を読み込めませんでした</p>
            <Button variant="secondary" size="sm" icon="refresh" onClick={posts.reload}>
              再読み込み
            </Button>
          </div>
        ) : posts.state.status === "unavailable" ? (
          <p className={styles.state} role="status">
            公開投稿は準備中です
          </p>
        ) : items.length === 0 ? (
          <p className={styles.state} role="status">
            まだ公開投稿はありません
          </p>
        ) : (
          <ul className={styles.list}>
            {items.slice(0, VISIBLE_LIMIT).map((post) => (
              <li key={post.uri} className={styles.item}>
                <p className={styles.text}>{post.text}</p>
                <span className={styles.meta}>
                  {now > 0 ? relativeTime(post.createdAt, now) : ""}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </section>
  );
}
