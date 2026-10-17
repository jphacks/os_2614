"use client";

import { useRouter, useSearchParams } from "next/navigation";
import type { Group, Post, Reply, User } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { ReplySheet } from "@/components/feature/post";
import { GroupScreen } from "./GroupScreen";

export type GroupReplyScreenProps = {
  /** GET /groups/{id} の取得状態。 */
  state: ResourceState<Group>;
  /** タイムライン (ドメイン変換済み)。返信画面が開いている投稿もここから探す。 */
  posts: Post[];
  /** GET /instances/{spaceId}/posts の取得状態。 */
  postsState?: ResourceState<Post[]>;
  /**
   * 返信一覧。backend の PostItem は reply:{root,parent} (10 §3) を返すので、
   * 呼び出し側が同じスレッドの返信だけを選んで渡す。
   */
  replies: Reply[];
  me: User;
  /** グループ取得の再実行 (useResource.reload)。 */
  groupRetry?: () => void;
  /** 投稿一覧の再実行 (usePagedResource.reload)。 */
  postsRetry?: () => void;
  /**
   * 返信の送信。POST /posts の reply:{root,parent} を呼ぶ。
   * 未指定なら ReplySheet が「準備中」を出す。
   */
  onReply?: (post: Post, text: string) => Promise<void>;
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
}: GroupReplyScreenProps) {
  const router = useRouter();
  const params = useSearchParams();
  const postId = params.get("post");
  const post = posts.find((item) => item.id === postId) ?? posts[0];
  const close = () =>
    router.push(closePath ?? `/group/?id=${params.get("id") ?? ""}`);

  return (
    <GroupScreen
      state={state}
      posts={posts}
      postsState={postsState}
      tabBar={false}
      groupRetry={groupRetry}
      postsRetry={postsRetry}
      overlay={
        post ? (
          <ReplySheet
            post={post}
            replies={replies}
            me={me}
            onClose={close}
            onSubmit={onReply ? (text) => onReply(post, text) : undefined}
          />
        ) : undefined
      }
    />
  );
}
