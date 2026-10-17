import { Icon } from "../Icon";
import styles from "./ImagePlaceholder.module.css";

export type ImagePlaceholderProps = {
  ratio?: string;
  src?: string;
  alt?: string;
  className?: string;
};

export function ImagePlaceholder({
  ratio = "4/3",
  src,
  alt = "",
  className,
}: ImagePlaceholderProps) {
  const classes = [styles.frame, className].filter(Boolean).join(" ");

  return (
    <span className={classes} style={{ aspectRatio: ratio }}>
      {src ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img className={styles.image} src={src} alt={alt} />
      ) : (
        <Icon name="image" size={24} className={styles.icon} />
      )}
    </span>
  );
}
