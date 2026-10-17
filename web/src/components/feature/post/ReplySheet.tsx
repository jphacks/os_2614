"use client";

import type { Post, Reply, User } from "@/domain/types";
import { Avatar } from "@/components/ui/Avatar";
import { Icon } from "@/components/ui/Icon";
import { Notice } from "@/components/ui/Notice";
import { clockTime } from "@/data/format";
import styles from "./ReplySheet.module.css";

function ReplyRow({ reply }: { reply: Reply }) {
  return (
    <li className={styles.reply}>
      <Avatar user={reply.author} size={40} />
      <div className={styles.replyMain}>
        <div className={styles.head}>
          <span className={styles.name}>{reply.author.name}</span>
          <span className={styles.time}>・{clockTime(reply.createdAt)}</span>
        </div>
        <p className={styles.body}>{reply.body}</p>
      </div>
    </li>
  );
}

export type ReplySheetProps = {
  post: Post;
  replies: Reply[];
  me: User;
  onClose: () => void;
};

export function ReplySheet({ post, replies, me, onClose }: ReplySheetProps) {
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
            </div>
          </div>
          <ul className={styles.replies}>
            {replies.map((reply) => (
              <ReplyRow key={reply.id} reply={reply} />
            ))}
          </ul>
        </div>

        {/* 返信の送信は backend 未対応（POST /posts に reply が無く、独立エンドポイントも無い）。
            無言の no-op を避けるため、composer は操作不能にして「準備中」を明示する。 */}
        <form
          className={styles.composer}
          onSubmit={(event) => event.preventDefault()}
        >
          <span className={styles.composerLabel}>{post.author.name} に返信</span>
          <div className={styles.composerRow}>
            <Avatar user={me} size={32} />
            <input
              className={styles.input}
              placeholder="返信を書く…"
              aria-label="返信を書く"
              disabled
            />
            <button
              type="submit"
              className={styles.send}
              aria-label="返信を送信"
              disabled
            >
              <Icon name="arrow_upward" size={20} />
            </button>
          </div>
          <Notice tone="info" icon="info">返信は準備中です</Notice>
        </form>
      </div>
    </div>
  );
}
