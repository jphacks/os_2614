import type { Attachment } from "@/domain/types";
import { ImagePlaceholder } from "@/components/ui/ImagePlaceholder";
import styles from "./PhotoGrid.module.css";

const MAX = 6;

const TONE_CLASS: Record<string, string> = {
  peach: styles.tonePeach,
  blue: styles.toneBlue,
  green: styles.toneGreen,
  lavender: styles.toneLavender,
  mint: styles.toneMint,
  yellow: styles.toneYellow,
  periwinkle: styles.tonePeriwinkle,
  sand: styles.toneSand,
};

export type PhotoGridProps = {
  photos: Attachment[];
  photoCount: number;
};

export function PhotoGrid({ photos, photoCount }: PhotoGridProps) {
  const cells = photos.slice(0, MAX);
  const overflow = photoCount - cells.length;
  const showOverlay = overflow > 0 && cells.length === MAX;

  return (
    <div className={styles.grid}>
      {cells.map((photo, index) => (
        <div key={photo.id} className={styles.cell}>
          <ImagePlaceholder ratio="6/5" src={photo.url || undefined} />
          {photo.by && (
            <span
              className={[styles.by, TONE_CLASS[photo.by.tone]]
                .filter(Boolean)
                .join(" ")}
              aria-hidden
            />
          )}
          {showOverlay && index === cells.length - 1 && (
            <span className={styles.overlay}>+{overflow}</span>
          )}
        </div>
      ))}
    </div>
  );
}
