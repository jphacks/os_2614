"use client";

import { useRouter } from "next/navigation";
import type { Group, Post, Reply, User } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { ReplySheet } from "@/components/feature/post";
import { GroupScreen } from "./GroupScreen";

export type GroupReplyScreenProps = {
  /** GET /groups/{id} の取得状態。 */
  state: ResourceState<Group>;
  /** タイムライン (ドメイン変換済み)。 */
  posts: Post[];
  /** GET /instances/{spaceId}/posts の取得状態。 */
  postsState?: ResourceState<Post[]>;
  /**
   * 返信一覧。backend の PostItem は reply:{root,parent} (10 §3) を返すので、
   * 呼び出し側が同じスレッドの返信だけを選んで渡す。
   */
  replies: Reply[];
  post: Post | null;
  replyTarget?: Pick<Post, "id"> | null;
  me: User;
  /** グループ取得の再実行 (useResource.reload)。 */
  groupRetry?: () => void;
  /** 投稿一覧の再実行 (usePagedResource.reload)。 */
  postsRetry?: () => void;
  /**
   * 返信の送信。POST /posts の reply:{root,parent} を呼ぶ。
   * 未指定なら ReplySheet が「準備中」を出す。
   */
  onReply?: (post: Pick<Post, "id">, text: string) => Promise<void>;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
  threadLoading?: boolean;
  threadNotFound?: boolean;
  threadError?: string;
  threadRetry?: () => void;
  canReply?: boolean;
  closePath?: string;
};

export function GroupReplyScreen({
  state,
  posts,
  postsState,
  replies,
  me,
  groupRetry,
  postsRetry,
  onReply,
  closePath,
  post,
  replyTarget,
  hasMore,
  loadingMore,
  onLoadMore,
  threadLoading,
  threadNotFound,
  threadError,
  threadRetry,
  canReply = true,
}: GroupReplyScreenProps) {
  const router = useRouter();
  const close = () =>
    router.push(closePath ?? "/group/");

  return (
    <GroupScreen
      state={state}
      posts={posts}
      postsState={postsState}
      tabBar={false}
      groupRetry={groupRetry}
      postsRetry={postsRetry}
      overlay={
        post || threadLoading || threadNotFound || threadError ? (
          <ReplySheet
            post={post}
            replies={replies}
            me={me}
            onClose={close}
            onSubmit={onReply && (replyTarget ?? post) && canReply
              ? (text) => onReply(replyTarget ?? post!, text)
              : undefined}
            hasMore={hasMore}
            loadingMore={loadingMore}
            onLoadMore={onLoadMore}
            loading={threadLoading}
            notFound={threadNotFound}
            threadError={threadError}
            onRetry={threadRetry}
            canReply={canReply}
          />
        ) : undefined
      }
    />
  );
}
