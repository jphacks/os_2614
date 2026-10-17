import styles from "./Badge.module.css";

export type BadgeProps = {
  label: string;
  tone: "positive";
  className?: string;
};

export function Badge({ label, tone, className }: BadgeProps) {
  return (
    <span className={[styles.badge, styles[tone], className].filter(Boolean).join(" ")}>
      {label}
    </span>
  );
}
