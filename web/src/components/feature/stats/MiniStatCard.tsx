import styles from "./MiniStatCard.module.css";

export type MiniStatCardProps = {
  label: string;
  value: string;
  note: string;
};

export function MiniStatCard({ label, value, note }: MiniStatCardProps) {
  return (
    <div className={styles.card}>
      <span className={styles.label}>{label}</span>
      <span className={styles.value}>{value}</span>
      <span className={styles.note}>{note}</span>
    </div>
  );
}
