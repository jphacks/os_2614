import styles from "./EncounterChart.module.css";

export type EncounterPoint = {
  label: string;
  value: number;
};

export type EncounterChartProps = {
  series: EncounterPoint[];
};

export function EncounterChart({ series }: EncounterChartProps) {
  const max = Math.max(1, ...series.map((point) => point.value));
  const lastIndex = series.length - 1;

  return (
    <div className={styles.chart}>
      <div className={styles.bars}>
        {series.map((point, index) => (
          <span
            key={`${point.label}-${index}`}
            className={[styles.bar, index === lastIndex && styles.today]
              .filter(Boolean)
              .join(" ")}
            style={{ height: `${(point.value / max) * 100}%` }}
            role="img"
            aria-label={`${point.label}: ${point.value}`}
          />
        ))}
      </div>
      {series.length > 0 && (
        <div className={styles.labels}>
          <span>{series[0].label}</span>
          <span>{series[lastIndex].label}</span>
        </div>
      )}
    </div>
  );
}
