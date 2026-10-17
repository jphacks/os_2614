"use client";

import type { ReactNode } from "react";
import Link from "next/link";
import { Icon } from "@/components/ui/Icon";
import styles from "./ScreenHeader.module.css";

export type ScreenHeaderProps = {
  title: string;
  subtitle?: string;
  back?: boolean | string;
  trailing?: ReactNode;
  count?: string;
  className?: string;
};

export function ScreenHeader({
  title,
  subtitle,
  back,
  trailing,
  count,
  className,
}: ScreenHeaderProps) {
  let backElement: ReactNode = null;
  if (typeof back === "string") {
    backElement = (
      <Link className={styles.back} href={back} aria-label="戻る">
        <Icon name="arrow_back_ios_new" size={20} />
      </Link>
    );
  } else if (back) {
    backElement = (
      <button
        type="button"
        className={styles.back}
        aria-label="戻る"
        onClick={() => window.history.back()}
      >
        <Icon name="arrow_back_ios_new" size={20} />
      </button>
    );
  }

  return (
    <header className={[styles.header, className].filter(Boolean).join(" ")}>
      <div className={styles.top}>
        {backElement}
        <span className={styles.titleWrap}>
          <span className={styles.title}>{title}</span>
          {count && <span className={styles.count}>{count}</span>}
        </span>
        {trailing && <span className={styles.trailing}>{trailing}</span>}
      </div>
      {subtitle && <p className={styles.subtitle}>{subtitle}</p>}
    </header>
  );
}
