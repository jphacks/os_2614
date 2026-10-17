import type { ReactNode } from "react";
import styles from "./Card.module.css";

export type CardProps = {
  as?: "section" | "div" | "li";
  padding?: "none" | "md";
  children: ReactNode;
  className?: string;
};

export function Card({
  as = "div",
  padding = "md",
  children,
  className,
}: CardProps) {
  const Tag = as;
  const classes = [styles.card, styles[padding], className]
    .filter(Boolean)
    .join(" ");

  return <Tag className={classes}>{children}</Tag>;
}
