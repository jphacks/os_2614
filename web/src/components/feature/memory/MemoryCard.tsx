import type { Memory } from "@/domain/types";
import { AvatarStack } from "@/components/ui/AvatarStack";
import { Card } from "@/components/ui/Card";
import { ImagePlaceholder } from "@/components/ui/ImagePlaceholder";
import { dateRange, durationLabel } from "@/data/format";
import styles from "./MemoryCard.module.css";

export type MemoryCardProps = {
  memory: Memory;
};

export function MemoryCard({ memory }: MemoryCardProps) {
  const overflow = memory.photoCount - 3;
  const realCount = overflow > 0 ? 3 : Math.min(4, memory.photos.length);
  const thumbs = memory.photos.slice(0, realCount);

  return (
    <Card padding="md" className={styles.card}>
      <div className={styles.thumbs}>
        {thumbs.map((photo) => (
          <ImagePlaceholder
            key={photo.id}
            ratio="1/1"
            src={photo.url || undefined}
          />
        ))}
        {overflow > 0 && (
          <span className={styles.overlay}>+{overflow}</span>
        )}
      </div>
      <div className={styles.foot}>
        <div className={styles.info}>
          <span className={styles.title}>{memory.title}</span>
          <span className={styles.meta}>
            {dateRange(memory.startedAt, memory.endedAt)}・
            {durationLabel(memory.startedAt, memory.endedAt)}・
            {memory.memberCount}人・写真 {memory.photoCount}枚
          </span>
        </div>
        <AvatarStack users={memory.members} />
      </div>
    </Card>
  );
}
