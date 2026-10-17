"use client";

import styles from "./Toggle.module.css";
import { Pressable } from "../Pressable";

export type ToggleProps = {
  checked: boolean;
  onChange: (checked: boolean) => void;
  /** aria-label */
  label: string;
  /** backend がオフを許さない設定など、切替不可の行に使う。 */
  disabled?: boolean;
  className?: string;
};

export function Toggle({
  checked,
  onChange,
  label,
  disabled = false,
  className,
}: ToggleProps) {
  const classes = [
    styles.toggle,
    checked && styles.on,
    disabled && styles.disabled,
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <Pressable
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      aria-disabled={disabled}
      disabled={disabled}
      className={classes}
      onClick={() => onChange(!checked)}
    >
      <span className={styles.thumb} />
    </Pressable>
  );
}
