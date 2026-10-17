import type { ReactNode } from "react";
import styles from "./List.module.css";

export type ListProps = {
  children: ReactNode;
  className?: string;
};

export function List({ children, className }: ListProps) {
  return (
    <ul className={[styles.list, className].filter(Boolean).join(" ")}>
      {children}
    </ul>
  );
}
