"use client";

import type { ReactNode } from "react";
import styles from "./List.module.css";
import { Pressable, PressableLink } from "../Pressable";

export type ListItemProps = {
  leading?: ReactNode;
  title: ReactNode;
  subtitle?: ReactNode;
  trailing?: ReactNode;
  /**
   * 行のリンクとは別に押せるボタン群。trailing の位置に出す。
   * href と併用すると <a> の中に <button> を入れずに済むよう、リンクを title に付けて行全体へ広げる。
   */
  actions?: ReactNode;
  href?: string;
  onClick?: () => void;
  className?: string;
};

export function ListItem({
  leading,
  title,
  subtitle,
  trailing,
  actions,
  href,
  onClick,
  className,
}: ListItemProps) {
  const rowClass = [styles.row, className].filter(Boolean).join(" ");
  const linkClass = [styles.link, subtitle && styles.withSubtitle]
    .filter(Boolean)
    .join(" ");

  const inner = (
    <>
      {leading && <span className={styles.leading}>{leading}</span>}
      <span className={styles.main}>
        <span className={styles.body}>
          <span className={styles.title}>{title}</span>
          {subtitle && <span className={styles.subtitle}>{subtitle}</span>}
        </span>
        {trailing && <span className={styles.trailing}>{trailing}</span>}
      </span>
    </>
  );

  if (href && actions) {
    return (
      <li className={[rowClass, styles.stretchedRow].join(" ")}>
        <span className={linkClass}>
          {leading && <span className={styles.leading}>{leading}</span>}
          <span className={styles.main}>
            <span className={styles.body}>
              <PressableLink className={[styles.title, styles.stretched].join(" ")} href={href}>
                {title}
              </PressableLink>
              {subtitle && <span className={styles.subtitle}>{subtitle}</span>}
            </span>
            <span className={[styles.trailing, styles.actions].join(" ")}>
              {trailing}
              {actions}
            </span>
          </span>
        </span>
      </li>
    );
  }

  if (href) {
    return (
      <li className={rowClass}>
        <PressableLink className={linkClass} href={href}>
          {inner}
        </PressableLink>
      </li>
    );
  }

  if (onClick) {
    return (
      <li className={rowClass}>
        <Pressable type="button" className={linkClass} onClick={onClick}>
          {inner}
        </Pressable>
      </li>
    );
  }

  return (
    <li className={rowClass}>
      <span className={linkClass}>{inner}</span>
    </li>
  );
}
