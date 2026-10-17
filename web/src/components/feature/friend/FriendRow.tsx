import type { Friend, LocationPrecision } from "@/domain/types";
import { Avatar } from "@/components/ui/Avatar";
import { Icon } from "@/components/ui/Icon";
import { IconButton } from "@/components/ui/IconButton";
import { ListItem } from "@/components/ui/List";
import styles from "./FriendRow.module.css";

/** 相手ごとの位置共有精度 (07 §4.1) の表示ラベル。 */
const PRECISION_LABEL: Record<LocationPrecision, string> = {
  exact: "正確な位置",
  approx: "おおよその位置",
  area: "エリアのみ",
  direction: "方角と距離",
  off: "位置を共有しない",
};

export type FriendRowProps = {
  friend: Friend;
  /** 友達から削除する (DELETE /friends/{did})。 */
  onDelete: (friend: Friend) => void;
  /** 削除リクエスト送信中はボタンを無効化する。 */
  deleting?: boolean;
};

export function FriendRow({ friend, onDelete, deleting = false }: FriendRowProps) {
  return (
    <ListItem
      leading={<Avatar user={friend} size={40} />}
      title={friend.name}
      subtitle={
        <>
          <span className={styles.handle}>@{friend.handle}</span>
          <span className={styles.sep} aria-hidden>
            ·
          </span>
          <span className={styles.precision}>{PRECISION_LABEL[friend.precision]}</span>
          <span className={styles.sep} aria-hidden>
            ·
          </span>
          {/*
            GET /friends/locations (07 §4.2) は未実装。エリア名を捏造せず、
            位置表示は「準備中」とする。相手ごとの精度は一覧 (GET /friends) が持つ。
          */}
          <span className={styles.location}>
            <Icon name="location_on" size={14} />
            <span>位置は準備中</span>
          </span>
        </>
      }
      trailing={
        <IconButton
          icon="person_remove"
          label={`${friend.name}を友達から削除`}
          onClick={() => onDelete(friend)}
          className={deleting ? styles.deleting : undefined}
        />
      }
    />
  );
}
