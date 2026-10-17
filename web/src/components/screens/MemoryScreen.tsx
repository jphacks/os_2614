"use client";

import { useState, type ReactNode } from "react";
import type { Memory } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { MemoryCard } from "@/components/feature/memory";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { SectionHeader } from "@/components/ui/SectionHeader";
import styles from "./MemoryScreen.module.css";

/** DELETE /memories/{spaceId} の結果。未実装 (404/405/501) は unavailable。 */
export type MemoryHideResult = "ok" | "unavailable" | "error";

export type MemoryScreenProps = {
  /**
   * GET /memories (13 §2) の取得状態。ok のときだけ memories を描画し、
   * loading / error / unavailable / empty は画面を空にせずそれぞれの状態を出す。
   */
  state: ResourceState<Memory[]>;
  /** 取得済みの一覧 (ページを跨いで蓄積)。ok / 追加取得中に描画する。 */
  memories: Memory[];
  /** backend がまだ次の cursor を返しているか。 */
  hasMore?: boolean;
  /** 追加ページの取得中か。 */
  loadingMore?: boolean;
  /** 次の 1 ページを取得する (「もっと見る」)。 */
  onLoadMore?: () => void;
  /** error 状態の再取得。usePagedResource の reload を渡す。 */
  onRetry?: () => void;
  /**
   * 1 件を自分の memory から隠す (DELETE /memories/{spaceId})。
   * 未実装の間は "unavailable" が返り、画面は準備中を出す (no-op で握り潰さない)。
   */
  onHide?: (spaceId: string) => Promise<MemoryHideResult>;
};

type HideNotice = {
  tone: "ok" | "unavailable" | "error";
  text: string;
};

export function MemoryScreen({
  state,
  memories,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  onRetry,
  onHide,
}: MemoryScreenProps) {
  const [busyId, setBusyId] = useState<string | null>(null);
  const [notice, setNotice] = useState<HideNotice | null>(null);

  // hide は押下時にだけ走る。effect 本体での setState はしない (react-hooks/set-state-in-effect 回避)。
  const handleHide = async (spaceId: string) => {
    if (!onHide || busyId !== null) {
      return;
    }
    setBusyId(spaceId);
    setNotice(null);
    const result = await onHide(spaceId);
    setBusyId(null);
    if (result === "ok") {
      setNotice({ tone: "ok", text: "メモリーから隠しました" });
      // 隠した 1 件を一覧から落とすため取り直す。
      onRetry?.();
    } else if (result === "unavailable") {
      setNotice({
        tone: "unavailable",
        text: "非表示は準備中です。しばらくお待ちください。",
      });
    } else {
      setNotice({
        tone: "error",
        text: "非表示にできませんでした。通信の状態を確認して、もう一度お試しください。",
      });
    }
  };

  const hasItems = state.status === "ok" && memories.length > 0;

  return (
    <AppShell tab="profile">
      <ScreenHeader
        back
        title="メモリー"
        subtitle="終わったパーティは、ここに残ります"
      />

      <div className={styles.content}>
        <section className={styles.section}>
          <SectionHeader
            title="アーカイブ"
            action={hasItems ? { label: "新しい順" } : undefined}
          />

          {notice && (
            <p
              className={styles.notice}
              data-tone={notice.tone}
              role="status"
              aria-live="polite"
            >
              {notice.text}
            </p>
          )}

          {state.status === "loading" ? (
            <MemorySkeleton />
          ) : state.status === "error" ? (
            <MemoryState
              icon="cloud_off"
              title="メモリーを読み込めませんでした"
              description="通信の状態を確認して、もう一度お試しください。"
              action={
                onRetry ? (
                  <Button variant="secondary" icon="refresh" onClick={onRetry}>
                    再読み込み
                  </Button>
                ) : undefined
              }
            />
          ) : state.status === "unavailable" ? (
            // 404/405/501 は「まだ無い / 未対応」で、エラーとは区別する。
            <MemoryState
              icon="construction"
              title="メモリーは準備中です"
              description="この機能はまだ提供されていません。"
            />
          ) : memories.length === 0 ? (
            <p className={styles.empty}>まだメモリーはありません</p>
          ) : (
            <>
              <div className={styles.list}>
                {memories.map((memory) => (
                  <MemoryCard
                    key={memory.spaceId}
                    memory={memory}
                    hideBusy={busyId === memory.spaceId}
                    onHide={
                      onHide ? () => void handleHide(memory.spaceId) : undefined
                    }
                  />
                ))}
              </div>
              {hasMore && (
                <div className={styles.more}>
                  <Button
                    variant="secondary"
                    disabled={loadingMore}
                    onClick={onLoadMore}
                  >
                    {loadingMore ? "読み込み中…" : "もっと見る"}
                  </Button>
                </div>
              )}
            </>
          )}
        </section>
      </div>
    </AppShell>
  );
}

type MemoryStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function MemoryState({ icon, title, description, action }: MemoryStateProps) {
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

function MemorySkeleton() {
  return (
    <div className={styles.skeleton} aria-hidden>
      <div className={styles.skeletonCover} />
      <div className={styles.skeletonLines}>
        <span className={styles.skeletonLineShort} />
        <span className={styles.skeletonLineLong} />
      </div>
    </div>
  );
}
