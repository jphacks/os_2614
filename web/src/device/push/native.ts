"use client";

import { PushNotifications } from "@capacitor/push-notifications";
import { setPushTokenProvider, syncDeviceRegistration } from "./index";

const REGISTRATION_TIMEOUT_MS = 10_000;

/** 12 §3: Android の通知チャンネルは social（フレンド・party・グループ・投稿）と digest（すれ違い）。 */
const CHANNELS = [
  { id: "social", name: "ソーシャル", description: "フレンド・パーティ・グループ・投稿の通知" },
  { id: "digest", name: "すれ違いのまとめ", description: "毎日・毎週・毎月のすれ違い" },
] as const;

let installed = false;
let latestToken: string | null = null;

/** 通知タップ時に渡す遷移先。data は backend の fcm.Message.Data（type / targetId / spaceId）。 */
export type PushTapHandler = (data: Record<string, string>) => void;

async function ensurePermission(): Promise<boolean> {
  let status = await PushNotifications.checkPermissions();
  if (status.receive === "prompt" || status.receive === "prompt-with-rationale") {
    status = await PushNotifications.requestPermissions();
  }
  return status.receive === "granted";
}

/**
 * FCM token の取得を push.ts の seam に差し込む。一度だけ呼ぶ。
 * token が後から更新されたとき（`registration` の再発火）は、端末登録を取り直す。
 */
export function installNativePush(onTap: PushTapHandler): void {
  if (installed) {
    return;
  }
  installed = true;

  void PushNotifications.addListener("registration", (token) => {
    latestToken = token.value;
    void syncDeviceRegistration().catch(() => undefined);
  });
  void PushNotifications.addListener("registrationError", () => {
    latestToken = null;
  });
  void PushNotifications.addListener("pushNotificationActionPerformed", (action) => {
    const data = action.notification.data as Record<string, string> | undefined;
    if (data) {
      onTap(data);
    }
  });

  setPushTokenProvider(async () => {
    if (latestToken) {
      return latestToken;
    }
    if (!(await ensurePermission())) {
      return null;
    }
    await Promise.all(CHANNELS.map((channel) => PushNotifications.createChannel(channel)));
    return new Promise<string | null>((resolve) => {
      const timer = setTimeout(() => resolve(latestToken), REGISTRATION_TIMEOUT_MS);
      void PushNotifications.addListener("registration", (token) => {
        clearTimeout(timer);
        resolve(token.value);
      });
      void PushNotifications.register();
    });
  });
}
