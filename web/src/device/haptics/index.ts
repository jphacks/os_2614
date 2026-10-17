// 触覚フィードバック (振動)。native (Android) は @capacitor/haptics 経由で
// 端末の触覚エンジンを使い、ブラウザでは同プラグインの web 実装が navigator.vibrate に落とす。
//
// 振動は演出なので、失敗しても呼び出し側には伝えない (await も不要)。
// vibrate 非対応のブラウザや、ユーザー操作前の呼び出しで弾かれても黙って何もしない。

import { Haptics, ImpactStyle, NotificationType } from "@capacitor/haptics";

export type HapticImpact = "light" | "medium" | "heavy";
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
