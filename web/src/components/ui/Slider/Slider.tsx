"use client";

import type { CSSProperties } from "react";
import styles from "./Slider.module.css";

export type SliderProps = {
  min: number;
  max: number;
  step: number;
  value: number;
  onChange: (value: number) => void;
  tone?: "primary" | "accent";
  marks?: string[];
  label?: string;
  className?: string;
};

export function Slider({
  min,
  max,
  step,
  value,
  onChange,
  tone = "primary",
  marks,
  label,
  className,
}: SliderProps) {
  const ratio = max === min ? 0 : (value - min) / (max - min);
  const percent = Math.min(100, Math.max(0, ratio * 100));

  const style = { "--slider-fill": `${percent}%` } as CSSProperties;

  const classes = [
    styles.slider,
    tone === "accent" ? styles.accent : styles.primary,
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={styles.wrap}>
      <input
        type="range"
        className={classes}
        style={style}
        min={min}
        max={max}
        step={step}
        value={value}
        aria-label={label}
        onChange={(event) => onChange(Number(event.target.value))}
      />
      {marks && marks.length > 0 && (
        <div className={styles.marks}>
          {marks.map((mark) => (
            <span key={mark} className={styles.mark}>
              {mark}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
