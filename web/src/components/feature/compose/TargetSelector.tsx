"use client";

import type { PostTarget } from "@/domain/types";
import { Icon } from "@/components/ui/Icon";
import styles from "./TargetSelector.module.css";

function iconFor(target: PostTarget): string {
  return target.kind === "group" ? "hub" : "celebration";
}

export type TargetSelectorProps = {
  target: PostTarget;
  onClick?: () => void;
};

export function TargetSelector({ target, onClick }: TargetSelectorProps) {
  return (
    <button
      type="button"
      className={styles.pill}
      onClick={onClick}
      aria-haspopup="menu"
    >
      <Icon name={iconFor(target)} size={18} />
      <span className={styles.name}>{target.name}</span>
      <Icon name="expand_more" size={18} />
    </button>
  );
}
