"use client";

import { useCallback, useState } from "react";
import type { GroupVote, GroupVoteKind, GroupVoteStatus } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { Notice } from "@/components/ui/Notice";
import { SectionHeader } from "@/components/ui/SectionHeader";
import { Tag } from "@/components/ui/Tag";
import styles from "./GroupVotesScreen.module.css";

/** 発議・投票の結果。unavailable はエンドポイント未対応 (404・405・501)。 */
export type GroupVoteActionResult =
  | { status: "ok" }
  | { status: "unavailable" }
  | { status: "error"; message: string };

export type GroupVotesScreenProps = {
  /** 対象のグループ (クエリ `?id=`)。 */
  groupId: string;
  /** GET /groups/{id}/votes (09 §8.6) の取得状態。 */
  votes: ResourceState<GroupVote[]>;
  /** POST /groups/{id}/votes {kind}。 */
  onPropose: (kind: GroupVoteKind) => Promise<GroupVoteActionResult>;
  /** POST /groups/{id}/votes/{voteId}/ballot {yes}。 */
  onBallot: (voteId: string, yes: boolean) => Promise<GroupVoteActionResult>;
  /** 取得に失敗したときの再読み込み。 */
  onRetry: () => void;
};

const KIND_LABEL: Record<GroupVoteKind, string> = {
  make_closed: "招待制にする",
  make_persistent: "常設にする",
};

const STATUS_LABEL: Record<GroupVoteStatus, string> = {
  open: "受付中",
  passed: "可決",
  failed: "否決",
};

/** closesAt (RFC 3339) を「M/D HH:mm」にする。不正・欠損なら空文字。 */
function formatDeadline(value: string): string {
  const at = new Date(value);
  if (Number.isNaN(at.getTime())) {
    return "";
  }
  const hh = String(at.getHours()).padStart(2, "0");
  const mm = String(at.getMinutes()).padStart(2, "0");
  return `${at.getMonth() + 1}/${at.getDate()} ${hh}:${mm}`;
}

export function GroupVotesScreen({
  groupId,
  votes,
  onPropose,
  onBallot,
  onRetry,
}: GroupVotesScreenProps) {
  const [pending, setPending] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);

  const propose = useCallback(
    async (kind: GroupVoteKind) => {
      if (pending !== null) {
        return;
      }
      setMessage(null);
      setPending(kind);
      const result = await onPropose(kind);
      setPending(null);
      if (result.status !== "ok") {
        setMessage(
          result.status === "unavailable"
            ? "投票の開始はまだ利用できません。"
            : result.message,
        );
      }
    },
    [onPropose, pending],
  );

  const castBallot = useCallback(
    async (voteId: string, yes: boolean) => {
      if (pending !== null) {
        return;
      }
      setMessage(null);
      setPending(voteId);
      const result = await onBallot(voteId, yes);
      setPending(null);
      if (result.status !== "ok") {
        setMessage(
          result.status === "unavailable"
            ? "投票はまだ利用できません。"
            : result.message,
        );
      }
    },
    [onBallot, pending],
  );

  // 一覧が ok / empty のときだけ、自分がメンバーだと確認できたものとして発議を出す。
  const canPropose = votes.status === "ok" || votes.status === "empty";

  return (
    <AppShell>
      <ScreenHeader
        title="投票"
        back={groupId ? `/group/?id=${groupId}` : "/"}
        subtitle="グループの設定を変える投票"
      />

      {message && (
        <p className={styles.message} role="status">
          {message}
        </p>
      )}

      {!groupId ? (
        <section className={styles.section}>
          <Card padding="md">
            <p className={styles.empty}>グループが指定されていません</p>
          </Card>
        </section>
      ) : (
        <>
          {canPropose && (
            <section className={styles.section}>
              <SectionHeader title="投票をはじめる" />
              <Card padding="md">
                <p className={styles.hint}>
                  48 時間以内に賛成がその時点のメンバーの過半数に達すると可決します。
                </p>
                <div className={styles.proposeRow}>
                  <Button
                    variant="secondary"
                    disabled={pending !== null}
                    onClick={() => void propose("make_closed")}
                  >
                    招待制にする
                  </Button>
                  <Button
                    variant="secondary"
                    disabled={pending !== null}
                    onClick={() => void propose("make_persistent")}
                  >
                    常設にする
                  </Button>
                </div>
              </Card>
            </section>
          )}

          <section className={styles.section}>
            <SectionHeader title="投票" />
            {votes.status === "ok" ? (
              <div className={styles.list}>
                {votes.data.map((vote) => (
                  <VoteCard
                    key={vote.id}
                    vote={vote}
                    pending={pending}
                    onBallot={castBallot}
                  />
                ))}
              </div>
            ) : votes.status === "empty" ? (
              <Card padding="md">
                <p className={styles.empty}>まだ投票はありません</p>
              </Card>
            ) : (
              <VotesState state={votes} onRetry={onRetry} />
            )}
          </section>
        </>
      )}

      <div className={styles.footer}>
        <Notice tone="info" icon="info">
          投票は何度でも入れ直せます。可決すると、このグループが「招待制」または「常設」に変わります。
        </Notice>
      </div>
    </AppShell>
  );
}

type VoteCardProps = {
  vote: GroupVote;
  pending: string | null;
  onBallot: (voteId: string, yes: boolean) => void;
};

function VoteCard({ vote, pending, onBallot }: VoteCardProps) {
  const deadline = formatDeadline(vote.closesAt);
  const isOpen = vote.status === "open";

  return (
    <Card padding="md" className={styles.voteCard}>
      <div className={styles.voteHead}>
        <span className={styles.voteTitle}>{KIND_LABEL[vote.kind]}</span>
        <Tag label={STATUS_LABEL[vote.status]} />
      </div>
      <p className={styles.counts}>
        賛成 {vote.yesCount} / 反対 {vote.noCount}
        <span className={styles.required}>
          （可決に必要な賛成 {vote.requiredCount}）
        </span>
      </p>
      <p className={styles.meta}>
        {deadline ? (isOpen ? `${deadline} まで` : `締め切り ${deadline}`) : "締め切り未定"}
        {vote.myBallot !== undefined
          ? `・あなたは${vote.myBallot ? "賛成" : "反対"}に投票済み`
          : ""}
      </p>
      {isOpen && (
        <div className={styles.voteActions}>
          <Button
            variant={vote.myBallot === true ? "primary" : "secondary"}
            size="sm"
            icon="check"
            disabled={pending !== null}
            onClick={() => onBallot(vote.id, true)}
          >
            賛成
          </Button>
          <Button
            variant={vote.myBallot === false ? "primary" : "secondary"}
            size="sm"
            icon="close"
            disabled={pending !== null}
            onClick={() => onBallot(vote.id, false)}
          >
            反対
          </Button>
        </div>
      )}
    </Card>
  );
}

type VotesStateProps = {
  state: ResourceState<GroupVote[]>;
  onRetry: () => void;
};

function VotesState({ state, onRetry }: VotesStateProps) {
  if (state.status === "loading") {
    return (
      <Card padding="md">
        <p className={styles.empty}>読み込み中…</p>
      </Card>
    );
  }
  if (state.status === "error") {
    return (
      <div className={styles.state}>
        <span className={styles.stateIcon} aria-hidden>
          <Icon name="cloud_off" size={28} />
        </span>
        <p className={styles.stateTitle}>投票を読み込めませんでした</p>
        <Button variant="secondary" icon="refresh" onClick={onRetry}>
          再読み込み
        </Button>
      </div>
    );
  }
  return (
    <Card padding="md">
      <p className={styles.empty}>投票は準備中です</p>
    </Card>
  );
}
