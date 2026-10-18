"use client";

import { useState, type FormEvent } from "react";
import type { Post, Reply, User } from "@/domain/types";
import { ApiError } from "@/auth/errors";
import { Avatar } from "@/components/ui/Avatar";
import { Icon } from "@/components/ui/Icon";
import { Notice } from "@/components/ui/Notice";
import { clockTime } from "@/data/format";
import styles from "./ReplySheet.module.css";
import { Pressable } from "@/components/ui/Pressable";

function ReplyRow({ reply }: { reply: Reply }) {
  return (
    <li className={styles.reply}>
      <Avatar user={reply.author} size={40} />
      <div className={styles.replyMain}>
        <div className={styles.head}>
          <span className={styles.name}>{reply.author.name}</span>
          <span className={styles.time}>・{clockTime(reply.createdAt)}</span>
        </div>
        {reply.parent && reply.root && reply.parent !== reply.root && (
          <p className={styles.time}>{reply.parentAuthorName ?? "投稿者"} への返信</p>
        )}
        <p className={styles.body}>{reply.body}</p>
      </div>
    </li>
  );
}

/** 送信失敗をユーザー向けの 1 行にする。backend の message を優先する。 */
function replyErrorText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 403) {
      return "この投稿には返信できません。";
    }
    if (error.status === 410 || error.status === 404) {
      return "この投稿は終了しているため返信できません。";
    }
    return error.message || "返信を送信できませんでした。";
  }
  return error instanceof Error && error.message
    ? error.message
    : "返信を送信できませんでした。";
}

export type ReplySheetProps = {
  post: Post | null;
  replies: Reply[];
  me: User;
  onClose: () => void;
  /**
   * 返信の送信。成功で resolve、失敗で reject する。
   * 未指定なら送信手段が無いので、composer を無効にして「準備中」を出す
   * (無言の no-op にしない)。
   */
  onSubmit?: (text: string) => Promise<void>;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
  loading?: boolean;
  notFound?: boolean;
  canReply?: boolean;
  threadError?: string;
  onRetry?: () => void;
};

export function ReplySheet({
  post,
  replies,
  me,
  onClose,
  onSubmit,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  loading = false,
  notFound = false,
  canReply = true,
  threadError,
  onRetry,
}: ReplySheetProps) {
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState<Reply | null>(null);

  const writable = onSubmit !== undefined;
  const canSend = writable && text.trim() !== "" && !sending;

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const value = text.trim();
    if (!onSubmit || value === "" || sending) {
      return;
    }
    setSending(true);
    setError(null);
    setPending({
      id: "pending",
      author: me,
      body: value,
      createdAt: new Date().toISOString(),
      likeCount: 0,
      likedByMe: false,
    });
    void onSubmit(value)
      .then(() => {
        setText("");
        setPending(null);
      })
      .catch((e: unknown) => {
        setPending(null);
        setError(replyErrorText(e));
      })
      .finally(() => {
        setSending(false);
      });
  };

  return (
    <div className={styles.overlay}>
      <div className={styles.sheet} role="dialog" aria-modal="true" aria-label="返信">
        <span className={styles.grabber} aria-hidden />
        <header className={styles.header}>
          <span className={styles.title}>返信</span>
          <Pressable
            type="button"
            className={styles.close}
            aria-label="閉じる"
            onClick={onClose}
          >
            <Icon name="close" size={22} />
          </Pressable>
        </header>

        <div className={styles.scroll}>
          {loading && <p className={styles.body}>読み込み中…</p>}
          {notFound && <p className={styles.body}>この投稿は見つかりません</p>}
          {threadError && (
            <Notice tone="warning" icon="error">
              {threadError}
              {onRetry && (
                <Pressable type="button" className={styles.send} onClick={onRetry}>
                  再試行
                </Pressable>
              )}
            </Notice>
          )}
          {!loading && !notFound && !threadError && post && <div className={styles.original}>
            <Avatar user={post.author} size={40} />
            <div className={styles.originalMain}>
              <div className={styles.head}>
                <span className={styles.name}>{post.author.name}</span>
                <span className={styles.time}>・{clockTime(post.createdAt)}</span>
              </div>
              <p className={styles.body}>{post.body}</p>
            </div>
          </div>}
          {!loading && !notFound && !threadError && <ul className={styles.replies}>
            {replies.map((reply) => (
              <ReplyRow key={reply.id} reply={reply} />
            ))}
            {pending && <ReplyRow reply={pending} />}
          </ul>}
          {hasMore && (
            <Pressable type="button" className={styles.send} onClick={onLoadMore} disabled={loadingMore}>
              {loadingMore ? "読み込み中…" : "もっと見る"}
            </Pressable>
          )}
        </div>

        {/* 返信は POST /posts の reply:{root,parent} (10 §3, 10 §4.1)。
            送信不可 (onSubmit 未指定) のときだけ操作不能 + 「準備中」を出す。 */}
        {canReply && !loading && !notFound && !threadError && <form className={styles.composer} onSubmit={submit}>
          <span className={styles.composerLabel}>{post ? `${post.author.name} に返信` : "返信"}</span>
          <div className={styles.composerRow}>
            <Avatar user={me} size={32} />
            <input
              className={styles.input}
              placeholder={writable ? "返信を書く…" : "返信は準備中です"}
              aria-label="返信を書く"
              value={text}
              onChange={(event) => setText(event.target.value)}
              disabled={!writable || sending}
              maxLength={2000}
            />
            <Pressable
              type="submit"
              className={styles.send}
              aria-label="返信を送信"
              disabled={!canSend}
            >
              <Icon name="arrow_upward" size={20} />
            </Pressable>
          </div>
          {error !== null && (
            <Notice tone="warning" icon="error">
              {error}
            </Notice>
          )}
          {!writable && (
            <Notice tone="info" icon="info">
              返信は準備中です
            </Notice>
          )}
        </form>}
      </div>
    </div>
  );
}
