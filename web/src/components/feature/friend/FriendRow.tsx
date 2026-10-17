import type { Friend } from "@/domain/types";
import { Avatar } from "@/components/ui/Avatar";
import { Icon } from "@/components/ui/Icon";
import { ListItem } from "@/components/ui/List";
import { relativeTime } from "@/data/format";
import styles from "./FriendRow.module.css";

const SUBTITLE_ICON = {
  area: "location_on",
  direction: "explore",
  hidden: "location_off",
} as const;

export type FriendRowProps = {
  friend: Friend;
  now: number | null;
};

export function FriendRow({ friend, now }: FriendRowProps) {
  if (friend.together) {
    return (
      <ListItem
        leading={<Avatar user={friend} size={40} online />}
        title={friend.name}
        subtitle={friend.locationLabel}
        trailing={<span className={styles.online}>いま</span>}
      />
    );
  }

  const subtitle =
    friend.locationVisibility === "hidden" ? (
      <>
        <Icon name="location_off" size={14} />
        <span>位置を見せていません</span>
      </>
    ) : (
      <>
        <Icon name={SUBTITLE_ICON[friend.locationVisibility]} size={14} />
        <span>{friend.locationLabel}</span>
      </>
    );

  return (
    <ListItem
      leading={<Avatar user={friend} size={40} />}
      title={friend.name}
      subtitle={subtitle}
      trailing={
        friend.lastSeenAt && now !== null ? (
          <span className={styles.time}>
            {relativeTime(friend.lastSeenAt, now)}
          </span>
        ) : undefined
      }
    />
  );
}
