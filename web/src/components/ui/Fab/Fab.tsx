"use client";

import { Icon } from "../Icon";
import styles from "./Fab.module.css";

export type FabProps = {
  icon?: "add";
  label: string;
  onClick: () => void;
  className?: string;
};

export function Fab({ icon = "add", label, onClick, className }: FabProps) {
  return (
    <button
      type="button"
      className={[styles.fab, className].filter(Boolean).join(" ")}
      aria-label={label}
      onClick={onClick}
    >
      <Icon name={icon} size={26} />
    </button>
  );
}
