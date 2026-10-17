"use client";

import { usePathname } from "next/navigation";
import { Icon } from "@/components/ui/Icon";
import styles from "./TabBar.module.css";
import { PressableLink } from "@/components/ui/Pressable";

export type TabKey = "nearby" | "party" | "friends" | "profile";

type TabDef = {
  key: TabKey;
  label: string;
  icon: string;
  href: string;
};

const TABS: TabDef[] = [
  { key: "nearby", label: "ちかく", icon: "hub", href: "/" },
  { key: "party", label: "パーティ", icon: "celebration", href: "/party/" },
  { key: "friends", label: "友達", icon: "group", href: "/friends/" },
  { key: "profile", label: "プロフィール", icon: "person", href: "/profile/" },
];

function activeKeyFor(pathname: string): TabKey | null {
  const match = TABS.find((tab) =>
    tab.href === "/" ? pathname === "/" : pathname.startsWith(tab.href),
  );
  return match ? match.key : null;
}

export type TabBarProps = {
  current?: TabKey;
};

export function TabBar({ current }: TabBarProps) {
  const pathname = usePathname();
  const active = current ?? activeKeyFor(pathname);

  return (
    <nav className={styles.bar} aria-label="メインナビゲーション">
      {TABS.map((tab) => {
        const selected = tab.key === active;
        return (
          <PressableLink
            key={tab.key}
            href={tab.href}
            className={[styles.tab, selected && styles.selected]
              .filter(Boolean)
              .join(" ")}
            aria-current={selected ? "page" : undefined}
          >
            <Icon name={tab.icon} size={26} filled={selected} />
            <span className={styles.label}>{tab.label}</span>
          </PressableLink>
        );
      })}
    </nav>
  );
}
