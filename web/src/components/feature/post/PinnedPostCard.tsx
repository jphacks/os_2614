import type { Post } from "@/domain/types";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import styles from "./PinnedPostCard.module.css";

export type PinnedPostCardProps = {
  post: Post;
};

export function PinnedPostCard({ post }: PinnedPostCardProps) {
  return (
    <Card padding="md" className={styles.card}>
      <div className={styles.head}>
        <Icon name="push_pin" size={16} />
        <span>自動ピン・{post.author.name}</span>
      </div>
      <p className={styles.body}>{post.body}</p>
    </Card>
  );
}
