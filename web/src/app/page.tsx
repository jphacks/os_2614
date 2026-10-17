import { AppShell } from "@/components/layout/AppShell";
import styles from "./page.module.css";

export default function NearbyPage() {
  return (
    <AppShell tab="nearby">
      <div className={styles.placeholder}>
        <p>ちかく（準備中）</p>
      </div>
    </AppShell>
  );
}
