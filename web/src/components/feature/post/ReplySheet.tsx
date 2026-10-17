"use client";

import { useState } from "react";
import type { Post, Reply, User } from "@/domain/types";
import { Avatar } from "@/components/ui/Avatar";
import { Icon } from "@/components/ui/Icon";
import { clockTime } from "@/data/format";
import styles from "./ReplySheet.module.css";

function ReplyRow({ reply }: { reply: Reply }) {
  const [likeOverride, setLikeOverride] = useState<boolean | null>(null);
  const liked = likeOverride ?? reply.likedByMe;
  const likeCount =
    reply.likeCount + (likeOverride === null ? 0 : likeOverride ? 1 : -1);

  return (
    <li className={styles.reply}>
      <Avatar user={reply.author} size={40} />
      <div className={styles.replyMain}>
        <div className={styles.head}>
          <span className={styles.name}>{reply.author.name}</span>
          <span className={styles.time}>・{clockTime(reply.createdAt)}</span>
        </div>
        <p className={styles.body}>{reply.body}</p>
        <button
          type="button"
          className={[styles.like, liked && styles.liked]
            .filter(Boolean)
            .join(" ")}
          aria-label="いいね"
          aria-pressed={liked}
          onClick={() => setLikeOverride(!liked)}
        >
          <Icon name="favorite" size={16} filled={liked} />
          <span>{likeCount}</span>
        </button>
      </div>
    </li>
  );
}

export type ReplySheetProps = {
  post: Post;
  replies: Reply[];
  me: User;
  onClose: () => void;
  onSubmit: (body: string) => void;
};

export function ReplySheet({
  post,
  replies,
  me,
  onClose,
  onSubmit,
}: ReplySheetProps) {
  const [body, setBody] = useState("");

  const handleSubmit = () => {
    const text = body.trim();
    if (text.length === 0) {
      return;
    }
    onSubmit(text);
    setBody("");
  };

  return (
    <div className={styles.overlay}>
      <div className={styles.sheet} role="dialog" aria-modal="true" aria-label="返信">
        <span className={styles.grabber} aria-hidden />
        <header className={styles.header}>
          <span className={styles.title}>返信</span>
          <button
            type="button"
            className={styles.close}
            aria-label="閉じる"
            onClick={onClose}
          >
            <Icon name="close" size={22} />
          </button>
        </header>

        <div className={styles.scroll}>
          <div className={styles.original}>
            <Avatar user={post.author} size={40} />
            <div className={styles.originalMain}>
              <div className={styles.head}>
                <span className={styles.name}>{post.author.name}</span>
                <span className={styles.time}>・{clockTime(post.createdAt)}</span>
              </div>
              <p className={styles.body}>{post.body}</p>
              <span className={styles.replyCount}>返信 {post.replyCount}件</span>
            </div>
          </div>
          <ul className={styles.replies}>
            {replies.map((reply) => (
              <ReplyRow key={reply.id} reply={reply} />
            ))}
          </ul>
        </div>

        <form
          className={styles.composer}
          onSubmit={(event) => {
            event.preventDefault();
            handleSubmit();
          }}
        >
          <span className={styles.composerLabel}>{post.author.name} に返信</span>
          <div className={styles.composerRow}>
            <Avatar user={me} size={32} />
            <input
              className={styles.input}
              value={body}
              placeholder="返信を書く…"
              aria-label="返信を書く"
              onChange={(event) => setBody(event.target.value)}
            />
            <button
              type="submit"
              className={styles.send}
              aria-label="返信を送信"
              disabled={body.trim().length === 0}
            >
              <Icon name="arrow_upward" size={20} />
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
