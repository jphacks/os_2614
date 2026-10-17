import type { User } from "@/domain/types";
import { Icon } from "../Icon";
import styles from "./Avatar.module.css";

export type AvatarProps = {
  user: Pick<User, "name" | "avatarUrl" | "tone">;
  size?: number;
  online?: boolean;
  ring?: boolean;
  dashed?: boolean;
  className?: string;
};

const TONE_CLASS = {
  peach: styles.tonePeach,
  blue: styles.toneBlue,
  green: styles.toneGreen,
  lavender: styles.toneLavender,
  mint: styles.toneMint,
  yellow: styles.toneYellow,
  periwinkle: styles.tonePeriwinkle,
  sand: styles.toneSand,
} as const;

export function Avatar({
  user,
  size = 32,
  online = false,
  ring = false,
  dashed = false,
  className,
}: AvatarProps) {
  const classes = [
    styles.avatar,
    TONE_CLASS[user.tone],
    online && styles.online,
    ring && styles.ring,
    dashed && styles.dashed,
    className,
  ]
    .filter(Boolean)
    .join(" ");

  const initial =
    user.name.trim().length > 0 ? Array.from(user.name.trim())[0] : "";

  return (
    <span
      className={classes}
      style={{ width: size, height: size }}
      role="img"
      aria-label={user.name}
    >
      {user.avatarUrl ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img className={styles.image} src={user.avatarUrl} alt="" />
      ) : dashed && initial === "" ? (
        <Icon name="add" size={Math.round(size * 0.5)} />
      ) : (
        <span className={styles.initial} style={{ fontSize: size * 0.4 }}>
          {initial}
        </span>
      )}
      {online && <span className={styles.dot} />}
    </span>
  );
}
