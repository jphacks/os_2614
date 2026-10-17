"use client";

import styles from "./SegmentedControl.module.css";
import { Pressable } from "../Pressable";

export type SegmentedOption<T extends string> = {
  value: T;
  label: string;
};

export type SegmentedControlProps<T extends string> = {
  options: SegmentedOption<T>[];
  value: T;
  onChange: (value: T) => void;
  /** 地の色。カード上なら "bg"、灰背景上なら "surface-muted" */
  track?: "bg" | "surface-muted";
  className?: string;
};

export function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  track = "surface-muted",
  className,
}: SegmentedControlProps<T>) {
  const classes = [
    styles.control,
    track === "bg" ? styles.trackBg : styles.trackSurface,
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={classes} role="radiogroup">
      {options.map((option) => {
        const selected = option.value === value;
        return (
          <Pressable
            key={option.value}
            type="button"
            role="radio"
            aria-checked={selected}
            className={[styles.segment, selected && styles.selected]
              .filter(Boolean)
              .join(" ")}
            onClick={() => onChange(option.value)}
          >
            {option.label}
          </Pressable>
        );
      })}
    </div>
  );
}
