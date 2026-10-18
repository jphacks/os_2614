import { IconButton } from "@/components/ui/IconButton";
import styles from "./StatusBadge.module.css";

export type StatusBadgeProps = {
  /** ステータス本文。空文字は未設定として扱う。 */
  status: string;
  /** 指定すると編集ボタンを出す (自分のプロフィール用)。省略時は読み取り専用。 */
  onEdit?: () => void;
};

/** 名前の直下に置くステータス pill (online 点 + 本文)。長文は 2 行まで折り返す。 */
export function StatusBadge({ status, onEdit }: StatusBadgeProps) {
  if (!status) {
    // 未設定: 本人にだけ「ステータスを設定」を出し、他人向けには何も描かない。
    if (!onEdit) {
      return null;
    }
    return (
      <button type="button" className={`${styles.badge} ${styles.empty}`} onClick={onEdit}>
        ステータスを設定
      </button>
    );
  }

  return (
    <span className={styles.badge}>
      <span className={styles.dot} aria-hidden />
      <span className={styles.text} title={status}>
        {status}
      </span>
      {onEdit && <IconButton icon="edit" label="ステータスを編集" onClick={onEdit} />}
    </span>
  );
}
