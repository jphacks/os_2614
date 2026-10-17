"use client";

import { useRouter, useSearchParams } from "next/navigation";
import type { Group, Post, Reply, User } from "@/domain/types";
import { ReplySheet } from "@/components/feature/post";
import { GroupScreen } from "./GroupScreen";

export type GroupReplyScreenProps = {
  group: Group;
  posts: Post[];
  replies: Reply[];
  me: User;
  onCompose?: (group: Group) => void;
};

export function GroupReplyScreen({
  group,
  posts,
  replies,
  me,
  onCompose,
}: GroupReplyScreenProps) {
  const router = useRouter();
  const params = useSearchParams();
  const postId = params.get("post");
  const post = posts.find((item) => item.id === postId) ?? posts[0];
  const close = () => router.push(`/group/?id=${group.id}`);

  return (
    <GroupScreen
      group={group}
      posts={posts}
      tabBar={false}
      onCompose={onCompose}
      overlay={
        post ? (
          <ReplySheet
            post={post}
            replies={replies}
            me={me}
            onClose={close}
            onSubmit={() => {}}
          />
        ) : undefined
      }
    />
  );
}
