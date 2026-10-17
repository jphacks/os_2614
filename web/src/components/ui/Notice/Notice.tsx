import type { ReactNode } from "react";
import { Icon } from "../Icon";
import styles from "./Notice.module.css";

export type NoticeProps = {
  icon?: string;
  children: ReactNode;
  tone?: "warning" | "info";
  className?: string;
};

export function Notice({
  icon,
  children,
  tone = "warning",
  className,
}: NoticeProps) {
  const classes = [styles.notice, styles[tone], className]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={classes}>
      {icon && <Icon name={icon} size={16} className={styles.icon} />}
      <span className={styles.text}>{children}</span>
    </div>
  );
}
