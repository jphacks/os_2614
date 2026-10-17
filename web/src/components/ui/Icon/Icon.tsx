import styles from "./Icon.module.css";

export type IconProps = {
  name: string;
  size?: number;
  filled?: boolean;
  className?: string;
};

export function Icon({ name, size = 24, filled = false, className }: IconProps) {
  const classes = [styles.icon, filled && styles.filled, className]
    .filter(Boolean)
    .join(" ");

  return (
    <span
      className={`material-symbols-rounded ${classes}`}
      style={{ fontSize: size }}
      aria-hidden
    >
      {name}
    </span>
  );
}
