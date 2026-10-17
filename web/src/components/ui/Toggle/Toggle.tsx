"use client";

import styles from "./Toggle.module.css";

export type ToggleProps = {
  checked: boolean;
  onChange: (checked: boolean) => void;
  /** aria-label */
  label: string;
  className?: string;
};

export function Toggle({ checked, onChange, label, className }: ToggleProps) {
  const classes = [styles.toggle, checked && styles.on, className]
    .filter(Boolean)
    .join(" ");

  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      className={classes}
      onClick={() => onChange(!checked)}
    >
      <span className={styles.thumb} />
    </button>
  );
}
