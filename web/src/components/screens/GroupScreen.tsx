"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import type { Group, Pin, Post } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { PinList } from "@/components/feature/pin";
import { PinnedPostCard, PostCard } from "@/components/feature/post";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Fab } from "@/components/ui/Fab";
import { IconButton } from "@/components/ui/IconButton";
import { Notice } from "@/components/ui/Notice";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Tag } from "@/components/ui/Tag";
import styles from "./GroupScreen.module.css";

type GroupTab = "timeline" | "pin" | "media" | "members";

const TABS: { value: GroupTab; label: string }[] = [
  { value: "timeline", label: "タイムライン" },
  { value: "pin", label: "ピン" },
  { value: "media", label: "メディア" },
  { value: "members", label: "メンバー" },
];

const EMPTY_LABEL: Record<GroupTab, string> = {
  timeline: "",
  pin: "まだピンされた投稿はありません",
  media: "まだメディアはありません",
  members: "まだメンバーはいません",
};

export type GroupScreenProps = {
  /**
   * GET /groups/{id} の取得状態。指定時はこのデータを描画の source にする
   * (loading / error / unavailable はヘッダー下にインライン表示する)。
   */
  state?: ResourceState<Group>;
  /** state 未指定時に使う取得済みグループ (返信画面のオーバーレイなど)。 */
  group?: Group;
  /** タイムラインに並べる投稿 (ドメイン変換済み)。 */
  posts: Post[];
  /**
   * GET /instances/{spaceId}/posts の取得状態。未指定なら posts をそのまま ok として描画する。
   * loading / error / unavailable はタイムライン内にインライン表示する。
   */
  postsState?: ResourceState<Post[]>;
  /** 続きのページがあるか (usePagedResource.hasMore)。 */
  hasMore?: boolean;
  /** 続きの取得中か (usePagedResource.loadingMore)。 */
  loadingMore?: boolean;
  /** 次のページを末尾に足す (usePagedResource.loadMore)。 */
  onLoadMore?: () => void;
  /** グループ取得の再実行 (useResource.reload)。 */
  groupRetry?: () => void;
  /** 投稿一覧の再実行 (usePagedResource.reload)。 */
  postsRetry?: () => void;
  /** Space の Pin 一覧 (GET /instances/{spaceId}/pins, 10 §6)。 */
  pins?: Pin[];
  /** Pin 一覧の取得状態。 */
  pinsState?: ResourceState<Pin[]>;
  /** Pin 一覧の再実行 (useResource.reload)。 */
  pinsRetry?: () => void;
  /** Pin / 解除の送信中。 */
  pinBusy?: boolean;
  /** 投稿を手動 Pin する。 */
  onPin?: (post: Post) => void;
  /** Pin を外す。 */
  onUnpin?: (pin: Pin) => void;
  /** Pin / 解除の直近の失敗。 */
  pinError?: string | null;
  tabBar?: boolean;
  overlay?: ReactNode;
  onCompose?: (group: Group) => void;
  onReply?: (post: Post) => void;
  onToggleLike?: (post: Post) => void;
};

export function GroupScreen({
  state,
  group: groupProp,
  posts,
  postsState,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  groupRetry,
  postsRetry,
  pins = [],
  pinsState,
  pinsRetry,
  pinBusy = false,
  onPin,
  onUnpin,
  pinError,
  tabBar = true,
  overlay,
  onCompose,
  onReply,
  onToggleLike,
}: GroupScreenProps) {
  const router = useRouter();
  const [tab, setTab] = useState<GroupTab>("timeline");

  // state があればそれを、無ければ group をそのまま ok として扱う (返信画面の互換)。
  const groupState: ResourceState<Group> =
    state ?? (groupProp ? { status: "ok", data: groupProp } : { status: "loading" });

  const { pinned, rest } = useMemo(() => {
    const pinnedPosts: Post[] = [];
    const restPosts: Post[] = [];
    for (const post of posts) {
      (post.pinned ? pinnedPosts : restPosts).push(post);
    }
    return { pinned: pinnedPosts, rest: restPosts };
  }, [posts]);

  if (groupState.status !== "ok") {
    return (
      <AppShell tab={tabBar ? "nearby" : undefined}>
        <ScreenHeader
          title="グループ"
          back
          trailing={<IconButton icon="more_horiz" label="グループのメニュー" />}
        />
        <div className={styles.gutter}>
          {groupState.status === "loading" ? (
            <Card padding="md" className={styles.empty}>
              読み込み中…
            </Card>
          ) : groupState.status === "error" ? (
            <Card padding="md" className={styles.stack}>
              <p className={styles.empty}>グループを読み込めませんでした</p>
              {groupRetry && (
                <Button variant="secondary" icon="refresh" onClick={groupRetry}>
                  再読み込み
                </Button>
              )}
            </Card>
          ) : (
            // empty / unavailable (404・405・501) は「まだ無い / 未対応」。
            <Card padding="md" className={styles.empty}>
              グループが見つかりません
            </Card>
          )}
        </div>
        {overlay}
      </AppShell>
    );
  }

  const group = groupState.data;

  const goCompose = () => {
    onCompose?.(group);
    router.push(`/compose/?target=group:${group.id}`);
  };

  const goReply = (post: Post) => {
    onReply?.(post);
    router.push(`/group/replies/?id=${group.id}&post=${post.id}`);
  };

  const postsStatus = postsState?.status ?? "ok";
  const pinState: ResourceState<Pin[]> =
    pinsState ?? { status: "ok", data: pins };

  return (
    <AppShell tab={tabBar ? "nearby" : undefined}>
      <ScreenHeader
        title={group.name}
        back
        trailing={
          <IconButton icon="more_horiz" label="グループのメニュー" />
        }
      />

      <div className={styles.tags}>
        <Tag label={group.visibility === "open" ? "オープン" : "招待制"} />
        <Tag label={`${group.memberCount}人`} />
        <Tag label={group.areaLabel} />
      </div>

      <div className={styles.gutter}>
        <Notice tone="warning" icon="location_on">
          {group.insideArea
            ? "エリアに入りました・エリア外では投稿できません"
            : "エリア外では投稿できません"}
        </Notice>
      </div>

      <div className={styles.gutter}>
        <SegmentedControl options={TABS} value={tab} onChange={setTab} />
      </div>

      {tab === "timeline" ? (
        <div className={styles.timeline}>
          {postsStatus === "loading" ? (
            <Card padding="md" className={styles.empty}>
              読み込み中…
            </Card>
          ) : postsStatus === "error" ? (
            <Card padding="md" className={styles.stack}>
              <p className={styles.empty}>投稿を読み込めませんでした</p>
              {postsRetry && (
                <Button variant="secondary" icon="refresh" onClick={postsRetry}>
                  再読み込み
                </Button>
              )}
            </Card>
          ) : postsStatus === "unavailable" ? (
            // 404・405・501 は「まだ無い / 未対応」。
            <Card padding="md" className={styles.empty}>
              まだ投稿はありません
            </Card>
          ) : posts.length === 0 ? (
            <Card padding="md" className={styles.empty}>
              まだ投稿はありません
            </Card>
          ) : (
            <>
              {pinned.map((post) => (
                <PinnedPostCard key={post.id} post={post} />
              ))}
              {rest.map((post) => (
                <PostCard
                  key={post.id}
                  post={post}
                  onReply={goReply}
                  onToggleLike={onToggleLike}
                />
              ))}
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
      ) : tab === "pin" ? (
        <div className={styles.timeline}>
          {pinError && (
            <Notice tone="warning" icon="error">
              {pinError}
            </Notice>
          )}
          <PinList
            state={pinState}
            pins={pins}
            posts={posts}
            onPin={(post) => onPin?.(post)}
            onUnpin={(pin) => onUnpin?.(pin)}
            onRetry={pinsRetry}
            busy={pinBusy}
          />
        </div>
      ) : (
        <div className={styles.gutter}>
          <Card padding="md" className={styles.empty}>
            {EMPTY_LABEL[tab]}
          </Card>
        </div>
      )}

      {group.insideArea && <Fab label="投稿する" onClick={goCompose} />}
      {overlay}
    </AppShell>
  );
}
