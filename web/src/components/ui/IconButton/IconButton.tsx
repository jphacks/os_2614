"use client";

import { Icon } from "../Icon";
import styles from "./IconButton.module.css";

export type IconButtonProps = {
  icon: string;
  label: string;
  variant?: "plain" | "filled" | "surface";
  onClick?: () => void;
  disabled?: boolean;
  className?: string;
};

export function IconButton({
  icon,
  label,
  variant = "plain",
  onClick,
  disabled = false,
  className,
}: IconButtonProps) {
  const classes = [styles.button, styles[variant], disabled && styles.disabled, className]
    .filter(Boolean)
    .join(" ");

  return (
    <button
      type="button"
      className={classes}
      aria-label={label}
      onClick={onClick}
      disabled={disabled}
    >
      <Icon name={icon} size={20} filled={variant === "filled"} />
    </button>
  );
}
