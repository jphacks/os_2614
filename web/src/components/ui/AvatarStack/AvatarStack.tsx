import type { User } from "@/domain/types";
import { Avatar } from "../Avatar";
import styles from "./AvatarStack.module.css";

export type AvatarStackProps = {
  users: User[];
  max?: number;
  size?: number;
  className?: string;
};

export function AvatarStack({
  users,
  max = 3,
  size = 24,
  className,
}: AvatarStackProps) {
  const shown = users.slice(0, max);
  const extra = users.length - shown.length;

  return (
    <span className={[styles.stack, className].filter(Boolean).join(" ")}>
      {extra > 0 && <span className={styles.extra}>+{extra}</span>}
      {shown.map((user) => (
        <span key={user.id} className={styles.item}>
          <Avatar user={user} size={size} />
        </span>
      ))}
    </span>
  );
}
