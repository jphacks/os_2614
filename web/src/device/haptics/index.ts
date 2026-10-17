// 触覚フィードバック (振動)。native (Android) は @capacitor/haptics 経由で
// 端末の触覚エンジンを使い、ブラウザでは同プラグインの web 実装が navigator.vibrate に落とす。
//
// 振動は演出なので、失敗しても呼び出し側には伝えない (await も不要)。
// vibrate 非対応のブラウザや、ユーザー操作前の呼び出しで弾かれても黙って何もしない。

import { Capacitor, registerPlugin } from "@capacitor/core";
import { Haptics, ImpactStyle, NotificationType } from "@capacitor/haptics";

interface ChainHapticsPlugin {
  contextClick(): Promise<void>;
}

// native (Android) だけが実装する。ブラウザ・iOS では呼ばず impact に落とす。
const ChainHaptics = registerPlugin<ChainHapticsPlugin>("ChainHaptics");

export type HapticImpact = "light" | "medium" | "heavy";
/** 押下時の触覚の種類。contextClick は Android のシステム触覚、それ以外は impact の強さ。 */
export type HapticPress = HapticImpact | "contextClick";
export type HapticNotification = "success" | "warning" | "error";

const IMPACT_STYLE: Record<HapticImpact, ImpactStyle> = {
  light: ImpactStyle.Light,
  medium: ImpactStyle.Medium,
  heavy: ImpactStyle.Heavy,
};

const NOTIFICATION_TYPE: Record<HapticNotification, NotificationType> = {
  success: NotificationType.Success,
  warning: NotificationType.Warning,
  error: NotificationType.Error,
};

function ignore(promise: Promise<void>): void {
  promise.catch(() => {
    // 振動できない環境では何もしない。
  });
}

/** ボタンを押したときなどの短い衝撃。既定は light。 */
export function hapticImpact(style: HapticImpact = "light"): void {
  ignore(Haptics.impact({ style: IMPACT_STYLE[style] }));
}

/** 操作の結果 (成功・警告・失敗) を伝えるパターン。 */
export function hapticNotification(type: HapticNotification): void {
  ignore(Haptics.notification({ type: NOTIFICATION_TYPE[type] }));
}

/** セグメントやスライダーで選択が切り替わったときの軽い刻み。 */
export function hapticSelection(): void {
  ignore(Haptics.selectionStart().then(() => Haptics.selectionChanged()).then(() => Haptics.selectionEnd()));
}

/**
 * ボタンを押したときの触覚。contextClick は Android で CONTEXT_CLICK を鳴らし、
 * 非対応の環境 (ブラウザ・iOS) では light の impact にする。
 */
export function hapticPress(kind: HapticPress = "contextClick"): void {
  if (kind !== "contextClick") {
    hapticImpact(kind);
    return;
  }
  if (Capacitor.getPlatform() === "android") {
    ignore(ChainHaptics.contextClick());
    return;
  }
  hapticImpact("light");
}
