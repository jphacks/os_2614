"use client";

import { useEffect } from "react";
import { removeDeviceRegistration, syncDeviceRegistration } from "./index";

/**
 * signedIn の間だけ端末登録を保つ。mount で `PUT /me/devices`（Android + token があるときだけ）、
 * signedOut への遷移で unmount するため cleanup で `DELETE /me/devices/{fcmToken}` を呼ぶ（12 §2）。
 * layout 直下に 1 つだけ置き、画面遷移で unmount しないようにする。
 */
export function DeviceRegistrar() {
  useEffect(() => {
    void syncDeviceRegistration().catch(() => undefined);
    return () => {
      void removeDeviceRegistration();
    };
  }, []);

  return null;
}
