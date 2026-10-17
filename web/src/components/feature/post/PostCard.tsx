"use client";

import { useState } from "react";
import type { Post } from "@/domain/types";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { ImagePlaceholder } from "@/components/ui/ImagePlaceholder";
import { Notice } from "@/components/ui/Notice";
import { clockTime } from "@/data/format";
import { MarkdownBody } from "./MarkdownBody";
import styles from "./PostCard.module.css";

export type PostCardProps = {
  post: Post;
  onReply?: (post: Post) => void;
  onToggleLike?: (post: Post) => void;
};

/** moderation の分類 (11 §1) の表示名。未知はコードのまま出す。 */
const CATEGORY_LABELS: Record<string, string> = {
  harassment: "嫌がらせ",
  hate: "ヘイト",
  sexual: "性的",
  violence: "暴力",
  self_harm: "自傷",
  personal_info: "個人情報",
  spam: "スパム",
};

function categoryLabel(category: string): string {
  return CATEGORY_LABELS[category] ?? category;
}

export function PostCard({ post, onReply, onToggleLike }: PostCardProps) {
  const [likeOverride, setLikeOverride] = useState<boolean | null>(null);
  const [revealed, setRevealed] = useState(false);
  const liked = likeOverride ?? post.likedByMe;
  const likeCount =
    post.likeCount + (likeOverride === null ? 0 : likeOverride ? 1 : -1);

  const handleLike = () => {
    setLikeOverride(!liked);
    onToggleLike?.(post);
  };

  // moderation の表示指示 (11 §4) に従う。display="hide" は一覧側で除外済み。
  const warned = post.moderation?.label === "warn";
  const blurred = warned && post.moderation?.display === "blur";
  const bodyHidden = blurred && !revealed;
  const categories = (post.moderation?.categories ?? []).filter(
    (category) => category !== "none",
  );

  return (
    <Card padding="md" className={styles.card}>
      <div className={styles.row}>
        <Avatar user={post.author} size={32} />
        <div className={styles.main}>
          <div className={styles.head}>
            <span className={styles.name}>{post.author.name}</span>
            <span className={styles.time}>{clockTime(post.createdAt)}</span>
          </div>
          {warned && (
            <Notice tone="warning" icon="report">
              モデレーションにより警告が付いています
              {categories.length > 0
                ? `（${categories.map(categoryLabel).join("・")}）`
                : ""}
            </Notice>
          )}
          <div className={bodyHidden ? styles.blurred : undefined}>
            {post.format === "markdown" ? (
              <MarkdownBody source={post.body} />
            ) : (
              <p className={styles.body}>{post.body}</p>
            )}
          </div>
          {bodyHidden && (
            <Button variant="secondary" size="sm" onClick={() => setRevealed(true)}>
              タップして表示
            </Button>
          )}
          {post.attachments.length > 0 && (
            <div className={styles.media}>
              {post.attachments.map((attachment) => (
                <ImagePlaceholder
                  key={attachment.id}
                  src={attachment.url || undefined}
                />
              ))}
            </div>
          )}
          <div className={styles.actions}>
            <button
              type="button"
              className={styles.action}
              aria-label="返信"
              onClick={() => onReply?.(post)}
            >
              <Icon name="chat_bubble" size={18} />
              <span>{post.replyCount}</span>
            </button>
            <button
              type="button"
              className={[styles.action, liked && styles.liked]
                .filter(Boolean)
                .join(" ")}
              aria-label="いいね"
              aria-pressed={liked}
              onClick={handleLike}
            >
              <Icon name="favorite" size={18} filled={liked} />
              <span>{likeCount}</span>
            </button>
          </div>
        </div>
      </div>
    </Card>
  );
}
