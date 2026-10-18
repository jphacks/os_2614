"use client";

import { useState } from "react";
import type { Friend, FriendLocation, LocationPrecision } from "@/domain/types";
import { Avatar } from "@/components/ui/Avatar";
import { Chip } from "@/components/ui/Chip";
import { Icon } from "@/components/ui/Icon";
import { IconButton } from "@/components/ui/IconButton";
import { ListItem } from "@/components/ui/List";
import { Toggle } from "@/components/ui/Toggle";
import styles from "./FriendRow.module.css";

/** 相手ごとの位置共有精度 (07 §4.1) の表示ラベル。 */
const PRECISION_LABEL: Record<LocationPrecision, string> = {
  exact: "正確な位置",
  approx: "おおよその位置",
  area: "エリアのみ",
  direction: "方角と距離",
  off: "位置を共有しない",
};

const PRECISION_OPTIONS: readonly LocationPrecision[] = [
  "exact",
  "approx",
  "area",
  "direction",
  "off",
];

/** 相手に見えている現在位置の 1 行 (07 §4.2)。stale は空文字にして行を出さない。 */
function locationText(location: FriendLocation | null): string {
  if (!location) {
    return "位置は準備中";
  }
  if (location.stale) {
    return "";
  }
  switch (location.precision) {
    case "off":
      return "位置を見せていません";
    case "area":
      return location.outside ? "エリア外" : location.areaLabel || "エリア外";
    case "direction":
      return location.viewerStale
        ? "方角には位置の共有が必要"
        : [location.direction, location.distanceBucket].filter(Boolean).join(" ");
    case "exact":
    case "approx":
      return "現在地を共有中";
  }
}

export type FriendRowProps = {
  friend: Friend;
  /** GET /friends/locations の該当 1 件。null は「位置を取得できない」。 */
  location?: FriendLocation | null;
  /** 友達から削除する (DELETE /friends/{did})。 */
  onDelete: (friend: Friend) => void;
  /** 削除リクエスト送信中はボタンを無効化する。 */
  deleting?: boolean;
  /** PUT /friends/{did}/location-share (07 §4.2)。指定時だけ編集 UI を出す。 */
  onShare?: (precision: LocationPrecision, areasOnly: boolean) => Promise<void>;
};

export function FriendRow({
  friend,
  location = null,
  onDelete,
  deleting = false,
  onShare,
}: FriendRowProps) {
  const [editing, setEditing] = useState(false);
  const [areasOnly, setAreasOnly] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const apply = async (precision: LocationPrecision, nextAreasOnly: boolean) => {
    if (!onShare || saving) {
      return;
    }
    setError(null);
    setSaving(true);
    try {
      await onShare(precision, nextAreasOnly);
    } catch {
      setError("位置の見せ方を変更できませんでした。");
    } finally {
      setSaving(false);
    }
  };

  // 会っていない友達 (met=false) には位置の設定を出さない (openness-implementation D6)。
  const text = friend.met ? locationText(location) : "";
  const canShare = friend.met && Boolean(onShare);

  return (
    <>
      <ListItem
        leading={<Avatar user={friend} size={40} />}
        title={friend.name}
        href={`/users/?did=${encodeURIComponent(friend.did)}&relation=friend`}
        subtitle={
          <>
            <span className={styles.handle}>@{friend.handle}</span>
            <span className={styles.sep} aria-hidden>
              ·
            </span>
            <span className={styles.precision}>
              {friend.met ? PRECISION_LABEL[friend.precision] : "Bluesky で相互フォロー"}
            </span>
            {text && (
              <>
                <span className={styles.sep} aria-hidden>
                  ·
                </span>
                <span className={styles.location}>
                  <Icon name="location_on" size={14} />
                  <span>{text}</span>
                </span>
              </>
            )}
          </>
        }
        actions={
          <span className={styles.trailing}>
            {canShare && (
              <IconButton
                icon="tune"
                label={`${friend.name}の位置の見せ方`}
                onClick={() => setEditing((prev) => !prev)}
              />
            )}
            <IconButton
              icon="person_remove"
              label={`${friend.name}を友達から削除`}
              onClick={() => onDelete(friend)}
              className={deleting ? styles.deleting : undefined}
            />
          </span>
        }
      />
      {editing && canShare && onShare && (
        <li className={styles.editor}>
          <p className={styles.editorLabel}>位置の見せ方</p>
          <div className={styles.chips}>
            {PRECISION_OPTIONS.map((precision) => (
              <Chip
                key={precision}
                label={PRECISION_LABEL[precision]}
                selected={friend.precision === precision}
                surface="surface"
                onClick={() => void apply(precision, areasOnly)}
              />
            ))}
          </div>
          <div className={styles.areasOnly}>
            <span className={styles.areasOnlyLabel}>決めたエリアだけで共有</span>
            <Toggle
              checked={areasOnly}
              label={`${friend.name}にはエリアだけで共有`}
              onChange={(next) => {
                setAreasOnly(next);
                void apply(friend.precision, next);
              }}
            />
          </div>
          {error && (
            <p className={styles.editorError} role="alert">
              {error}
            </p>
          )}
        </li>
      )}
    </>
  );
}
