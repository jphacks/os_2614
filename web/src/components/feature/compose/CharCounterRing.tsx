import styles from "./CharCounterRing.module.css";

export type CharCounterRingProps = {
  value: number;
  max: number;
};

const RADIUS = 9;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

export function CharCounterRing({ value, max }: CharCounterRingProps) {
  const ratio = max === 0 ? 0 : Math.min(1, Math.max(0, value / max));
  const offset = CIRCUMFERENCE * (1 - ratio);

  return (
    <svg
      className={styles.ring}
      width={24}
      height={24}
      viewBox="0 0 24 24"
      aria-hidden
    >
      <circle className={styles.track} cx={12} cy={12} r={RADIUS} />
      <circle
        className={styles.bar}
        cx={12}
        cy={12}
        r={RADIUS}
        strokeDasharray={CIRCUMFERENCE}
        strokeDashoffset={offset}
      />
    </svg>
  );
}
