import type { ReactNode } from "react";
import { TabBar, type TabKey } from "../TabBar";
import styles from "./AppShell.module.css";

export type AppShellProps = {
  tab?: TabKey;
  children: ReactNode;
  className?: string;
};

export function AppShell({ tab, children, className }: AppShellProps) {
  const classes = [styles.shell, className].filter(Boolean).join(" ");
  const mainClasses = [styles.main, tab && styles.withTab]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={classes}>
      <main className={mainClasses}>{children}</main>
      {tab && <TabBar current={tab} />}
    </div>
  );
}
