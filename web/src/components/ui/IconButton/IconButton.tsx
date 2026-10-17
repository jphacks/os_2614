"use client";

import { Icon } from "../Icon";
import styles from "./IconButton.module.css";

export type IconButtonProps = {
  icon: string;
  label: string;
  variant?: "plain" | "filled" | "surface";
  onClick?: () => void;
  className?: string;
};

export function IconButton({
  icon,
  label,
  variant = "plain",
  onClick,
  className,
}: IconButtonProps) {
  const classes = [styles.button, styles[variant], className]
    .filter(Boolean)
    .join(" ");

  return (
    <button
      type="button"
      className={classes}
      aria-label={label}
      onClick={onClick}
    >
      <Icon name={icon} size={20} filled={variant === "filled"} />
    </button>
  );
}
