"use client";

import { Icon } from "@/components/ui/Icon";
import styles from "./TargetSelector.module.css";
import { Pressable } from "@/components/ui/Pressable";

export type TargetSelectorProps = {
  /** 選択中の送信先を要約した表示名（複数選択では「名前 他N件」）。 */
  label: string;
  /** 先頭に出す material symbol 名。 */
  icon?: string;
  onClick?: () => void;
};

export function TargetSelector({ label, icon = "hub", onClick }: TargetSelectorProps) {
  return (
    <Pressable
      type="button"
      className={styles.pill}
      onClick={onClick}
      aria-haspopup="menu"
    >
      <Icon name={icon} size={18} />
      <span className={styles.name}>{label}</span>
      <Icon name="expand_more" size={18} />
    </Pressable>
  );
}
