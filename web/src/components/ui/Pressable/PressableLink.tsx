"use client";

import Link from "next/link";
import type { ComponentProps, PointerEvent } from "react";
import { hapticPress, type HapticPress } from "@/device/haptics";

export type PressableLinkProps = ComponentProps<typeof Link> & {
  /** 押した瞬間の振動。false で振動させない。既定は contextClick */
  haptic?: HapticPress | false;
};

/** 押すと振動する next/link。ボタンの Pressable と同じ振動を遷移リンクにも付ける。 */
export function PressableLink({ haptic = "contextClick", onPointerDown, ...rest }: PressableLinkProps) {
  const handlePointerDown = (event: PointerEvent<HTMLAnchorElement>) => {
    onPointerDown?.(event);
    if (haptic && event.button === 0) {
      hapticPress(haptic);
    }
  };
  return <Link {...rest} onPointerDown={handlePointerDown} />;
}
