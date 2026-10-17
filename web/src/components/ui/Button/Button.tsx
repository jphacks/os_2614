"use client";

import type { ReactNode } from "react";
import { Icon } from "../Icon";
import styles from "./Button.module.css";

export type ButtonProps = {
  variant: "primary" | "secondary" | "ghost";
  size?: "md" | "sm";
  icon?: string;
  children: ReactNode;
  onClick?: () => void;
  disabled?: boolean;
  type?: "button" | "submit";
  className?: string;
};

export function Button({
  variant,
  size = "md",
  icon,
  children,
  onClick,
  disabled = false,
  type = "button",
  className,
}: ButtonProps) {
  const classes = [
    styles.button,
    styles[variant],
    styles[size],
    disabled && styles.disabled,
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <button
      type={type}
      className={classes}
      onClick={onClick}
      disabled={disabled}
    >
      {icon && <Icon name={icon} size={size === "sm" ? 18 : 20} />}
      <span>{children}</span>
    </button>
  );
}
