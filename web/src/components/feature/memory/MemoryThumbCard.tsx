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
        src={memory.cover?.thumbUrl || undefined}
        className={styles.thumb}
      />
      <span className={styles.title}>{memory.name}</span>
      <span className={styles.meta}>
        {durationLabel(memory.from, memory.until)}・写真 {memory.mediaCount}枚
      </span>
    </div>
  );
}
