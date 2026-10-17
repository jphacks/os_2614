import styles from "./ActivityGrid.module.css";

export type ActivityGridProps = {
  activity: number[][];
  messagesThisMonth: number;
};

function levelClass(level: number): string {
  const clamped = Math.min(4, Math.max(0, Math.round(level)));
  return styles[`level${clamped}`];
}

export function ActivityGrid({
  activity,
  messagesThisMonth,
}: ActivityGridProps) {
  const days = activity.length > 0 ? activity[0].length : 7;

  return (
    <div className={styles.card}>
      <div className={styles.body}>
        <div className={styles.grid} aria-hidden>
          {Array.from({ length: days }, (_, day) =>
            activity.map((week, weekIndex) => (
              <span
                key={`${weekIndex}-${day}`}
                className={[styles.cell, levelClass(week[day] ?? 0)].join(" ")}
              />
            )),
          )}
        </div>
        <div className={styles.side}>
          <span className={styles.sideLabel}>今月のメッセージ</span>
          <span className={styles.sideValue}>{messagesThisMonth}</span>
        </div>
      </div>
      <p className={styles.caption}>
        直近12週・パーティとグループでの発言
      </p>
    </div>
  );
}
