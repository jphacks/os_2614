"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { destinationOf } from "@/components/feature/notification/destination";
import { isAndroid } from "./index";

/**
 * Android のときだけ push の token 取得を有効にし、通知タップで該当画面へ遷移する。
 * DeviceRegistrar より先に動かす必要があるので、layout で DeviceRegistrar の前に置く。
 */
export function PushBootstrap() {
  const router = useRouter();
  useEffect(() => {
    if (!isAndroid()) {
      return;
    }
    void import("./native").then(({ installNativePush }) => {
      installNativePush((data) => {
        router.push(destinationOf(data.type ?? "", data));
      });
    });
  }, [router]);
  return null;
}

