"use client";

import { useState, type ButtonHTMLAttributes, type PointerEvent, type Ref } from "react";
import { hapticImpact, type HapticImpact } from "@/device/haptics";
import styles from "./Pressable.module.css";

export type PressableProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  /** 押した瞬間の振動。false で振動させない。既定は light */
  haptic?: HapticImpact | false;
  ref?: Ref<HTMLButtonElement>;
};

/**
 * 押すと凹んで振動する <button>。見た目 (色・サイズ) は持たないので className で与える。
 * :active は Android WebView だと反応が遅れるため、pointer イベントで押下状態を持つ。
 */
export function Pressable({
  haptic = "light",
  type = "button",
  className,
  disabled,
  onPointerDown,
  onPointerUp,
  onPointerLeave,
  onPointerCancel,
  ...rest
}: PressableProps) {
  const [pressed, setPressed] = useState(false);

  const handlePointerDown = (event: PointerEvent<HTMLButtonElement>) => {
    onPointerDown?.(event);
    // マウスは主ボタンだけ。タッチ・ペンは button が常に 0
    if (disabled || event.button !== 0) {
      return;
    }
    setPressed(true);
    if (haptic) {
      hapticImpact(haptic);
    }
  };

  const release =
    (handler?: (event: PointerEvent<HTMLButtonElement>) => void) =>
    (event: PointerEvent<HTMLButtonElement>) => {
      handler?.(event);
      setPressed(false);
    };

  return (
    <button
      {...rest}
      type={type}
      disabled={disabled}
      className={[styles.pressable, className].filter(Boolean).join(" ")}
      data-pressed={pressed && !disabled ? "" : undefined}
      onPointerDown={handlePointerDown}
      onPointerUp={release(onPointerUp)}
      onPointerLeave={release(onPointerLeave)}
      onPointerCancel={release(onPointerCancel)}
    />
  );
}
