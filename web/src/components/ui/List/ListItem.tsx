"use client";

import type { ReactNode } from "react";
import Link from "next/link";
import styles from "./List.module.css";

export type ListItemProps = {
  leading?: ReactNode;
  title: ReactNode;
  subtitle?: ReactNode;
  trailing?: ReactNode;
  href?: string;
  onClick?: () => void;
  className?: string;
};

export function ListItem({
  leading,
  title,
  subtitle,
  trailing,
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

  if (href) {
    return (
      <li className={rowClass}>
        <Link className={linkClass} href={href}>
          {inner}
        </Link>
      </li>
    );
  }

  if (onClick) {
    return (
      <li className={rowClass}>
        <button type="button" className={linkClass} onClick={onClick}>
          {inner}
        </button>
      </li>
    );
  }

  return (
    <li className={rowClass}>
      <span className={linkClass}>{inner}</span>
    </li>
  );
}
