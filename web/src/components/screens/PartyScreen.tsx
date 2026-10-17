"use client";

import { useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import type { Media, Party, Post } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { MemberStrip, PartyHero, PhotoGrid } from "@/components/feature/party";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Fab } from "@/components/ui/Fab";
import { Icon } from "@/components/ui/Icon";
import { IconButton } from "@/components/ui/IconButton";
import { Notice } from "@/components/ui/Notice";
import { Toggle } from "@/components/ui/Toggle";
import styles from "./PartyScreen.module.css";
import { PostCard } from "@/components/feature/post";

/** 変更系 API（leave / lock / rename）の分類結果。unavailable は 404/405/501（未実装）。 */
export type PartyActionResult =
  | { status: "ok" }
  | { status: "unavailable" }
  | { status: "error" };

export type PartyActionKind = "lock" | "unlock" | "leave" | "rename";

export type PartyActionNotice = {
  kind: PartyActionKind;
  status: "unavailable" | "error";
};

export type PartyScreenProps = {
  /** GET /party/current の取得状態。ok のときだけ data（マップ済み Party）を描画する。 */
  state: ResourceState<Party>;
  /** パーティの Space に投稿された写真（GET /instances/{spaceId}/posts 由来）。 */
  photos: Media[];
  posts: Post[];
  postsState?: ResourceState<Post[]>;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
  /** 自分の DID。合流メンバーの表示から自分を除くために使う。 */
  meDid?: string;
  /** error 状態の再取得。useResource の reload を渡す。 */
  onRetry?: () => void;
  onLeave?: (party: Party) => Promise<PartyActionResult>;
  onToggleLock?: (party: Party, locked: boolean) => Promise<PartyActionResult>;
  onRename?: (party: Party, name: string) => Promise<PartyActionResult>;
  onCompose?: (party: Party) => void;
  onReply?: (post: Post) => void;
  endedNotice?: boolean;
};

export function PartyScreen({
  state,
  photos,
  posts,
  postsState,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  meDid,
  onRetry,
  onLeave,
  onToggleLock,
  onRename,
  onCompose,
  onReply,
  endedNotice = false,
}: PartyScreenProps) {
  return (
    <AppShell tab="party">
      {state.status === "ok" ? (
        <PartyBody
          key={state.data.id}
          party={state.data}
          photos={photos}
          posts={posts}
          postsState={postsState}
          hasMore={hasMore}
          loadingMore={loadingMore}
          onLoadMore={onLoadMore}
          meDid={meDid}
          onLeave={onLeave}
          onToggleLock={onToggleLock}
          onRename={onRename}
          onCompose={onCompose}
          onReply={onReply}
        />
      ) : state.status === "loading" ? (
        <PartySkeleton />
      ) : state.status === "error" ? (
        <PartyState
          icon="cloud_off"
          title="パーティを読み込めませんでした"
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
        // 404/405/501 は「まだ無い / 未対応」。
        <PartyState
          icon="construction"
          title="パーティ機能は準備中です"
          description="この機能はまだ提供されていません。"
        />
      ) : (
        // empty（GET /party/current が null）は「所属中の party が無い」。
        <>
          {endedNotice && (
            <div className={styles.gutter}>
              <Notice tone="info" icon="check_circle">
                パーティが終わりました。メモリーに残っています
              </Notice>
            </div>
          )}
          <PartyState
            icon="group"
            title="今はパーティに参加していません"
            description="近くのフレンドと会うとパーティが始まります。"
          />
        </>
      )}
    </AppShell>
  );
}

type PartyBodyProps = {
  party: Party;
  photos: Media[];
  posts: Post[];
  postsState?: ResourceState<Post[]>;
  hasMore: boolean;
  loadingMore: boolean;
  onLoadMore?: () => void;
  meDid?: string;
  onLeave?: PartyScreenProps["onLeave"];
  onToggleLock?: PartyScreenProps["onToggleLock"];
  onRename?: PartyScreenProps["onRename"];
  onCompose?: PartyScreenProps["onCompose"];
  onReply?: PartyScreenProps["onReply"];
};

const NOTICE_LABEL: Record<PartyActionKind, string> = {
  lock: "固定",
  unlock: "固定の解除",
  leave: "退出",
  rename: "名前の変更",
};

function noticeText(notice: PartyActionNotice): string {
  if (notice.status === "unavailable") {
    return `${NOTICE_LABEL[notice.kind]}は準備中です`;
  }
  return `${NOTICE_LABEL[notice.kind]}に失敗しました。もう一度お試しください。`;
}

/** 表示名。displayName → handle → did の順で落とす。 */
function memberName(
  member: Party["members"][number],
): string {
  return member.displayName || member.handle || member.did;
}

function PartyBody({
  party,
  photos,
  posts,
  postsState,
  hasMore,
  loadingMore,
  onLoadMore,
  meDid,
  onLeave,
  onToggleLock,
  onRename,
  onCompose,
  onReply,
}: PartyBodyProps) {
  const router = useRouter();
  // 固定トグルは backend が成功を返したときだけ切り替える（楽観更新しない）。
  // 表示は prop の party.locked に追従させ、楽観的な上書きだけを「元にした値」でキー付け
  // する。こうすると party.updated で locked が変わっても古い表示が残らない。
  const [lockOverride, setLockOverride] = useState<{
    base: boolean;
    value: boolean;
  } | null>(null);
  const locked =
    lockOverride !== null && lockOverride.base === party.locked
      ? lockOverride.value
      : party.locked;
  const [busy, setBusy] = useState<PartyActionKind | null>(null);
  const [notice, setNotice] = useState<PartyActionNotice | null>(null);
  const [renaming, setRenaming] = useState(false);

  const companions = party.members
    .filter((member) => member.did !== meDid && !member.left)
    .map(memberName)
    .join("・");

  const handleToggleLock = async (next: boolean) => {
    if (busy !== null || !onToggleLock) {
      return;
    }
    const kind: PartyActionKind = next ? "lock" : "unlock";
    setBusy(kind);
    setNotice(null);
    const result = await onToggleLock(party, next);
    setBusy(null);
    if (result.status === "ok") {
      setLockOverride({ base: party.locked, value: next });
      return;
    }
    setNotice({ kind, status: result.status });
  };

  const handleLeave = async () => {
    if (busy !== null || !onLeave) {
      return;
    }
    setBusy("leave");
    setNotice(null);
    const result = await onLeave(party);
    setBusy(null);
    if (result.status !== "ok") {
      setNotice({ kind: "leave", status: result.status });
    }
  };

  const handleRename = async (name: string) => {
    if (busy !== null || !onRename) {
      return;
    }
    setBusy("rename");
    setNotice(null);
    const result = await onRename(party, name);
    setBusy(null);
    if (result.status === "ok") {
      setRenaming(false);
      return;
    }
    setNotice({ kind: "rename", status: result.status });
  };

  const handleCompose = () => {
    onCompose?.(party);
    router.push(`/compose/?target=party:${party.id}`);
  };

  const subtitle = companions
    ? `${companions} と合流しました`
    : party.members.length > 1
      ? `${party.members.length}人で合流しています`
      : "パーティを組んでいます";
  const tentative = party.phase === "tentative";

  return (
    <>
      <header className={styles.header}>
        {renaming ? (
          <RenameForm
            initial={party.name}
            busy={busy === "rename"}
            onSubmit={handleRename}
            onCancel={() => setRenaming(false)}
          />
        ) : (
          <div className={styles.titleRow}>
            <h1 className={styles.title}>{party.name}</h1>
            {!tentative && onRename && (
              <IconButton
                icon="edit"
                label="パーティ名を変更"
                onClick={() => setRenaming(true)}
              />
            )}
          </div>
        )}
        <p className={styles.subtitle}>{subtitle}</p>
      </header>

      <div className={styles.gutter}>
        <PartyHero party={party} />
      </div>

      <div className={styles.gutter}>
        <Card padding="md">
          <div className={styles.row}>
            <Icon name="push_pin" size={22} />
            <div className={styles.rowText}>
              <span className={styles.rowTitle}>パーティを固定</span>
              <span className={styles.rowNote}>
                {tentative
                  ? "本パーティになると固定できます"
                  : "旅行のときなど、はぐれても解散しません"}
              </span>
            </div>
            <Toggle
              checked={locked}
              onChange={handleToggleLock}
              label="パーティを固定"
              disabled={tentative || busy !== null}
            />
          </div>
        </Card>
      </div>

      {notice && (
        <div className={styles.gutter}>
          <Notice
            tone={notice.status === "unavailable" ? "info" : "warning"}
            icon={notice.status === "unavailable" ? "construction" : "error"}
          >
            {noticeText(notice)}
          </Notice>
        </div>
      )}

      <div className={styles.section}>
        <div className={styles.sectionHead}>
          <h2 className={styles.sectionTitle}>メンバー {party.members.length}</h2>
        </div>
      </div>
      <div className={styles.members}>
        <MemberStrip members={party.members} />
      </div>

      {!tentative && (
        <div className={styles.section}>
          <div className={styles.sectionHead}>
            <h2 className={styles.sectionTitle}>共有された写真</h2>
            <span className={styles.hint}>{photos.length}枚</span>
          </div>
        </div>
      )}
      {tentative ? (
        <div className={styles.gutter}>
          <Card padding="md" className={styles.empty}>
            本パーティになると、写真や投稿を共有できます
          </Card>
        </div>
      ) : (
        <>
          <div className={styles.gutter}>
            {photos.length > 0 ? (
              <PhotoGrid photos={photos} />
            ) : (
              <p className={styles.photoEmpty}>まだ写真はありません</p>
            )}
          </div>
          <div className={styles.timeline}>
            {postsState?.status === "loading" ? (
              <Card padding="md" className={styles.empty}>
                読み込み中…
              </Card>
            ) : postsState?.status === "error" ? (
              <Card padding="md" className={styles.empty}>
                投稿を読み込めませんでした
              </Card>
            ) : posts.length === 0 ? (
              <Card padding="md" className={styles.empty}>
                まだ投稿はありません
              </Card>
            ) : (
              <>
                {posts.map((post) =>
                  post.system ? (
                    <p key={post.id} className={styles.systemPost}>
                      {post.body}
                    </p>
                  ) : (
                    <PostCard key={post.id} post={post} onReply={onReply} />
                  ),
                )}
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
              </>
            )}
          </div>
        </>
      )}

      <div className={styles.gutter}>
        <Card padding="md">
          <div className={styles.row}>
            <Icon name="logout" size={22} />
            <div className={styles.rowText}>
              <span className={styles.rowTitle}>パーティから退出</span>
              <span className={styles.rowNote}>
                このパーティのメンバーから外れます
              </span>
            </div>
            <Button
              variant="secondary"
              size="sm"
              onClick={handleLeave}
              disabled={busy === "leave"}
            >
              {busy === "leave" ? "退出中…" : "退出"}
            </Button>
          </div>
        </Card>
      </div>

      {!tentative && <Fab label="投稿する" onClick={handleCompose} />}
    </>
  );
}

type RenameFormProps = {
  initial: string;
  busy: boolean;
  onSubmit: (name: string) => void;
  onCancel: () => void;
};

function RenameForm({ initial, busy, onSubmit, onCancel }: RenameFormProps) {
  const [name, setName] = useState(initial);

  return (
    <form
      className={styles.renameForm}
      onSubmit={(event) => {
        event.preventDefault();
        const value = name.trim();
        if (value === "") {
          onCancel();
          return;
        }
        onSubmit(value);
      }}
    >
      <input
        className={styles.renameInput}
        value={name}
        onChange={(event) => setName(event.target.value)}
        maxLength={60}
        aria-label="パーティ名"
        autoFocus
      />
      <Button variant="primary" size="sm" type="submit" disabled={busy}>
        保存
      </Button>
      <Button variant="ghost" size="sm" onClick={onCancel} disabled={busy}>
        取消
      </Button>
    </form>
  );
}

type PartyStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function PartyState({ icon, title, description, action }: PartyStateProps) {
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

function PartySkeleton() {
  return (
    <div className={styles.skeleton} aria-hidden>
      <div className={styles.skeletonHero} />
      <div className={styles.skeletonCard} />
      <div className={styles.skeletonStrip} />
    </div>
  );
}
