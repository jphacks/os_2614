"use client";

import { Icon } from "../Icon";
import styles from "./Chip.module.css";
import { Pressable } from "../Pressable";

export type ChipProps = {
  label: string;
  selected?: boolean;
  icon?: string;
  onClick?: () => void;
  /** 未選択時の地。灰背景の上なら "bg"、白カードの上なら "surface" */
  surface?: "bg" | "surface";
  className?: string;
};

export function Chip({
  label,
  selected = false,
  icon,
  onClick,
  surface = "bg",
  className,
}: ChipProps) {
  const classes = [
    styles.chip,
    selected && styles.selected,
    !selected && styles[surface],
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <Pressable
      type="button"
      className={classes}
      onClick={onClick}
      aria-pressed={selected}
    >
      {icon && <Icon name={icon} size={16} />}
      <span className={styles.label}>{label}</span>
    </Pressable>
  );
}
