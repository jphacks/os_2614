"use client";

import { Icon } from "@/components/ui/Icon";
import styles from "./VisibilityOption.module.css";
import { Pressable } from "@/components/ui/Pressable";

export type VisibilityOptionProps = {
  icon: string;
  title: string;
  description: string;
  selected: boolean;
  tone?: "notice" | "accent" | "primary";
  onSelect: () => void;
};

export function VisibilityOption({
  icon,
  title,
  description,
  selected,
  tone = "notice",
  onSelect,
}: VisibilityOptionProps) {
  return (
    <Pressable
      type="button"
      role="radio"
      aria-checked={selected}
      className={styles.option}
      onClick={onSelect}
    >
      <span className={[styles.icon, styles[tone]].join(" ")}>
        <Icon name={icon} size={20} />
      </span>
      <span className={styles.text}>
        <span className={styles.title}>{title}</span>
        <span className={styles.description}>{description}</span>
      </span>
      <span className={[styles.radio, selected && styles.radioOn].join(" ")}>
        {selected && <span className={styles.dot} />}
      </span>
    </Pressable>
  );
}
