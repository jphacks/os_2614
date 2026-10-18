import type { CSSProperties } from "react";
import { Icon } from "@/components/ui/Icon";
import type { AvatarTone } from "@/domain/types";
import type { NearbyBubble as Bubble } from "./model";
import styles from "./NearbyBubble.module.css";

export type NearbyBubbleProps = {
  bubble: Bubble;
  /** BubbleCanvas の中央に来ている。丸から角丸の四角に変わる。 */
  focused: boolean;
  /** 現れるアニメーションの遅れ (ms)。 */
  delay?: number;
  /** true の間は現れるアニメーションを止めておく (起動スプラッシュの裏で終わらせない)。 */
  paused?: boolean;
};

const tone = (value: AvatarTone) => `var(--tone-${value})`;

function initialOf(name: string) {
  const trimmed = name.trim();
  return trimmed.length > 0 ? Array.from(trimmed)[0] : "";
}

/** 「ちかく」の泡 1 つ。大きさは BubbleCanvas の item が決めるので、ここでは 100% で埋める。 */
export function NearbyBubble({ bubble, focused, delay = 0, paused = false }: NearbyBubbleProps) {
  return (
    <span
      className={styles.appear}
      style={{ animationDelay: `${delay}ms`, animationPlayState: paused ? "paused" : "running" }}
    >
      <BubbleBody bubble={bubble} focused={focused} />
    </span>
  );
}

function BubbleBody({ bubble, focused }: NearbyBubbleProps) {
  const focus = focused || undefined;

  switch (bubble.kind) {
    case "plus":
      return (
        <span className={`${styles.shape} ${styles.plus}`} data-focused={focus} aria-label="グループを作る">
          <Icon name="add" size={30} />
        </span>
      );
    case "party":
      return (
        <span className={styles.wrap}>
          <span className={`${styles.shape} ${styles.party}`} data-focused={focus}>
            <Icon name="celebration" size={26} />
            <span className={styles.count}>{bubble.memberCount}人</span>
          </span>
          <span className={styles.label}>{bubble.name}</span>
        </span>
      );
    case "group":
      return (
        <span
          className={`${styles.shape} ${styles.group}`}
          data-focused={focus}
          data-member={bubble.isMember || undefined}
          style={{ "--bubble-tone": tone(bubble.tone) } as CSSProperties}
        >
          <span className={styles.groupName}>{bubble.name}</span>
          <span className={styles.count}>{bubble.memberCount}人</span>
        </span>
      );
    case "me":
    case "person": {
      const user = bubble.user;
      const ringTone = bubble.kind === "person" ? bubble.groupTone : undefined;
      const style = {
        "--bubble-tone": tone(user.tone),
        ...(ringTone ? { "--bubble-ring": tone(ringTone) } : null),
      } as CSSProperties;
      return (
        <span className={styles.wrap}>
          <span
            className={`${styles.shape} ${styles.avatar}`}
            data-focused={focus}
            data-me={bubble.kind === "me" || undefined}
            data-ring={ringTone ? "" : undefined}
            data-encounter={(bubble.kind === "person" && bubble.source === "encounter") || undefined}
            style={style}
            role="img"
            aria-label={bubble.kind === "me" ? "自分" : user.name}
          >
            {user.avatarUrl ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img className={styles.image} src={user.avatarUrl} alt="" draggable={false} />
            ) : (
              <span className={styles.initial}>{initialOf(user.name)}</span>
            )}
          </span>
          <span className={styles.label}>{bubble.kind === "me" ? "じぶん" : user.name}</span>
        </span>
      );
    }
  }
}
