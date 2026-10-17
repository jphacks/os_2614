"use client";

import { useState } from "react";
import type { Post } from "@/domain/types";
import { Avatar } from "@/components/ui/Avatar";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { ImagePlaceholder } from "@/components/ui/ImagePlaceholder";
import { clockTime } from "@/data/format";
import { MarkdownBody } from "./MarkdownBody";
import styles from "./PostCard.module.css";

export type PostCardProps = {
  post: Post;
  onReply?: (post: Post) => void;
  onToggleLike?: (post: Post) => void;
};

export function PostCard({ post, onReply, onToggleLike }: PostCardProps) {
  const [likeOverride, setLikeOverride] = useState<boolean | null>(null);
  const liked = likeOverride ?? post.likedByMe;
  const likeCount =
    post.likeCount + (likeOverride === null ? 0 : likeOverride ? 1 : -1);

  const handleLike = () => {
    setLikeOverride(!liked);
    onToggleLike?.(post);
  };

  return (
    <Card padding="md" className={styles.card}>
      <div className={styles.row}>
        <Avatar user={post.author} size={32} />
        <div className={styles.main}>
          <div className={styles.head}>
            <span className={styles.name}>{post.author.name}</span>
            <span className={styles.time}>{clockTime(post.createdAt)}</span>
          </div>
          {post.format === "markdown" ? (
            <MarkdownBody source={post.body} />
          ) : (
            <p className={styles.body}>{post.body}</p>
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
