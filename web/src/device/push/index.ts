"use client";

import { Capacitor } from "@capacitor/core";
import { deleteJson, putJson } from "@/data/api";

const TOKEN_KEY = "chain.push.fcmToken.v1";

type TokenProvider = () => Promise<string | null>;

// Capacitor の push-notifications プラグインは同梱していないため、既定では token を
// 取得できない。Android 側に実プラグインが入った時点で setPushTokenProvider で差し替える。
let tokenProvider: TokenProvider = async () => null;

export function setPushTokenProvider(provider: TokenProvider): void {
  tokenProvider = provider;
}

export function isAndroid(): boolean {
  return Capacitor.getPlatform() === "android";
}

function readStoredToken(): string | null {
  try {
    return window.localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

function storeToken(token: string): void {
  try {
    window.localStorage.setItem(TOKEN_KEY, token);
  } catch {
    // 保存できない環境では登録済みフラグを持たない（次回も PUT する）。
  }
}

function clearStoredToken(): void {
  try {
    window.localStorage.removeItem(TOKEN_KEY);
  } catch {
    // no-op
  }
}

/**
 * Android かつ FCM token があるときだけ `PUT /me/devices`（12 §2）で端末を登録する。
 * ブラウザ・token 未取得では何もしない（Android 以外で登録しない）。
 */
export async function syncDeviceRegistration(): Promise<void> {
  if (!isAndroid()) {
    return;
  }
  const token = await tokenProvider();
  if (!token || readStoredToken() === token) {
    return;
  }
  await putJson<void>("/me/devices", { fcmToken: token, platform: "android" });
  storeToken(token);
}

/** 保存済み token があれば `DELETE /me/devices/{fcmToken}`（12 §2）で解除する。 */
export async function removeDeviceRegistration(): Promise<void> {
  const token = readStoredToken();
  clearStoredToken();
  if (!token) {
    return;
  }
  try {
    await deleteJson<void>(`/me/devices/${encodeURIComponent(token)}`);
  } catch {
    // ログアウトを妨げない。
  }
}
