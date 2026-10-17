import type { Memory } from "@/domain/types";
import { Card } from "@/components/ui/Card";
import { ImagePlaceholder } from "@/components/ui/ImagePlaceholder";
import { dateRange, durationLabel } from "@/data/format";
import styles from "./MemoryCard.module.css";

export type MemoryCardProps = {
  memory: Memory;
};

// 13 §2 の Memory は cover 1 枚と mediaCount / memberCount だけを持つ。
// 複数サムネ・メンバー一覧の本格表示はメモリー画面の todo で扱う。
export function MemoryCard({ memory }: MemoryCardProps) {
  return (
    <Card padding="md" className={styles.card}>
      <div className={styles.thumbs}>
        <ImagePlaceholder
          ratio="1/1"
          src={memory.cover?.thumbUrl || undefined}
        />
      </div>
      <div className={styles.foot}>
        <div className={styles.info}>
          <span className={styles.title}>{memory.name}</span>
          <span className={styles.meta}>
            {dateRange(memory.from, memory.until)}・
            {durationLabel(memory.from, memory.until)}・
            {memory.memberCount}人・写真 {memory.mediaCount}枚
          </span>
        </div>
      </div>
    </Card>
  );
}
