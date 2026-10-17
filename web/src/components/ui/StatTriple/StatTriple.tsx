import styles from "./StatTriple.module.css";

export type StatTripleItem = {
  value: number | string;
  label: string;
};

export type StatTripleProps = {
  items: StatTripleItem[];
  className?: string;
};

export function StatTriple({ items, className }: StatTripleProps) {
  return (
    <div
      className={[styles.card, className].filter(Boolean).join(" ")}
      style={{ gridTemplateColumns: `repeat(${items.length}, 1fr)` }}
    >
      {items.map((item, index) => (
        <div key={`${item.label}-${index}`} className={styles.col}>
          <span className={styles.value}>
            {typeof item.value === "number"
              ? item.value.toLocaleString("ja-JP")
              : item.value}
          </span>
          <span className={styles.label}>{item.label}</span>
        </div>
      ))}
    </div>
  );
}
