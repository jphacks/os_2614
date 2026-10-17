import type { Memory } from "@/domain/types";
import { ImagePlaceholder } from "@/components/ui/ImagePlaceholder";
import { durationLabel } from "@/data/format";
import styles from "./MemoryThumbCard.module.css";

export type MemoryThumbCardProps = {
  memory: Memory;
};

export function MemoryThumbCard({ memory }: MemoryThumbCardProps) {
  return (
    <div className={styles.card}>
      <ImagePlaceholder
        ratio="1/1"
        src={memory.photos[0]?.url || undefined}
        className={styles.thumb}
      />
      <span className={styles.title}>{memory.title}</span>
      <span className={styles.meta}>
        {durationLabel(memory.startedAt, memory.endedAt)}・写真 {memory.photoCount}枚
      </span>
    </div>
  );
}
