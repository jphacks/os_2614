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
   * 返信一覧。backend の PostItem には reply フィールドが無いため、
   * 独立したスレッド構造は持たない (API が返した一覧をそのまま描画する)。
   */
  replies: Reply[];
  me: User;
  /** グループ取得の再実行 (useResource.reload)。 */
  groupRetry?: () => void;
  /** 投稿一覧の再実行 (usePagedResource.reload)。 */
  postsRetry?: () => void;
};

export function GroupReplyScreen({
  state,
  posts,
  postsState,
  replies,
  me,
  groupRetry,
  postsRetry,
}: GroupReplyScreenProps) {
  const router = useRouter();
  const params = useSearchParams();
  const postId = params.get("post");
  const post = posts.find((item) => item.id === postId) ?? posts[0];
  const close = () => router.push(`/group/?id=${params.get("id") ?? ""}`);

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
          <ReplySheet post={post} replies={replies} me={me} onClose={close} />
        ) : undefined
      }
    />
  );
}
