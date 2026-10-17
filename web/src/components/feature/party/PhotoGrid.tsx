import type { Media } from "@/domain/types";
import { ImagePlaceholder } from "@/components/ui/ImagePlaceholder";
import styles from "./PhotoGrid.module.css";

const MAX = 6;

export type PhotoGridProps = {
  photos: Media[];
};

export function PhotoGrid({ photos }: PhotoGridProps) {
  const cells = photos.slice(0, MAX);
  const overflow = photos.length - cells.length;

  return (
    <div className={styles.grid}>
      {cells.map((photo, index) => (
        <div key={photo.id} className={styles.cell}>
          <ImagePlaceholder
            ratio="6/5"
            src={photo.thumbUrl || photo.url || undefined}
            alt={photo.alt}
          />
          {overflow > 0 && index === cells.length - 1 && (
            <span className={styles.overlay}>+{overflow}</span>
          )}
        </div>
      ))}
    </div>
  );
}
