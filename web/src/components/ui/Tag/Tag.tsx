import styles from "./Tag.module.css";

export type TagProps = {
  label: string;
  className?: string;
};

export function Tag({ label, className }: TagProps) {
  return (
    <span className={[styles.tag, className].filter(Boolean).join(" ")}>
      {label}
    </span>
  );
}
