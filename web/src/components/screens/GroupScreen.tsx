"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import type { Group, Post } from "@/domain/types";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { PinnedPostCard, PostCard } from "@/components/feature/post";
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
  group: Group;
  posts: Post[];
  tabBar?: boolean;
  overlay?: ReactNode;
  onCompose?: (group: Group) => void;
  onReply?: (post: Post) => void;
  onToggleLike?: (post: Post) => void;
};

export function GroupScreen({
  group,
  posts,
  tabBar = true,
  overlay,
  onCompose,
  onReply,
  onToggleLike,
}: GroupScreenProps) {
  const router = useRouter();
  const [tab, setTab] = useState<GroupTab>("timeline");

  const { pinned, rest } = useMemo(() => {
    const pinnedPosts: Post[] = [];
    const restPosts: Post[] = [];
    for (const post of posts) {
      (post.pinned ? pinnedPosts : restPosts).push(post);
    }
    return { pinned: pinnedPosts, rest: restPosts };
  }, [posts]);

  const goCompose = () => {
    onCompose?.(group);
    router.push(`/compose/?target=group:${group.id}`);
  };

  const goReply = (post: Post) => {
    onReply?.(post);
    router.push(`/group/replies/?id=${group.id}&post=${post.id}`);
  };

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
