"use client";

import styles from "./SectionHeader.module.css";
import { Pressable, PressableLink } from "../Pressable";

export type SectionHeaderAction = {
  label: string;
  href?: string;
  onClick?: () => void;
};

export type SectionHeaderProps = {
  title: string;
  action?: SectionHeaderAction;
  className?: string;
};

export function SectionHeader({
  title,
  action,
  className,
}: SectionHeaderProps) {
  return (
    <div className={[styles.header, className].filter(Boolean).join(" ")}>
      <h2 className={styles.title}>{title}</h2>
      {action &&
        (action.href ? (
          <PressableLink className={styles.action} href={action.href}>
            {action.label}
          </PressableLink>
        ) : (
          <Pressable type="button" className={styles.action} onClick={action.onClick}>
            {action.label}
          </Pressable>
        ))}
    </div>
  );
}
