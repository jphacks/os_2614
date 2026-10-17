import type { Memory } from "@/domain/types";
import { Card } from "@/components/ui/Card";
import { IconButton } from "@/components/ui/IconButton";
import { ImagePlaceholder } from "@/components/ui/ImagePlaceholder";
import { dateRange, durationLabel } from "@/data/format";
import styles from "./MemoryCard.module.css";

export type MemoryCardProps = {
  memory: Memory;
  /** 自分の memory から隠す (DELETE /memories/{spaceId})。未指定ならボタンを出さない。 */
  onHide?: () => void;
  /** 非表示リクエストの送信中か。二重送信の見た目を抑える。 */
  hideBusy?: boolean;
};

// 13 §2 の Memory は cover 1 枚と mediaCount / memberCount だけを持つ。
// 複数サムネ・メンバー一覧は backend に無いため描画しない (members[]/photos[] 前提にしない)。
export function MemoryCard({ memory, onHide, hideBusy = false }: MemoryCardProps) {
  return (
    <Card padding="md" className={styles.card}>
      <div className={styles.cover}>
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
        {onHide && (
          <IconButton
            icon="visibility_off"
            label="メモリーから隠す"
            variant="surface"
            onClick={onHide}
            className={hideBusy ? styles.busy : undefined}
          />
        )}
      </div>
    </Card>
  );
}
