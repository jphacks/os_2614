"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
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
        router.push(destinationOf(data));
      });
    });
  }, [router]);
  return null;
}

/** 通知 data から開く画面を決める（12 §4）。 */
export function destinationOf(data: Record<string, string>): string {
  switch (data.type) {
    case "friend.request":
      return "/friends/requests";
    case "party.confirmed":
      return "/party";
    case "post.new":
    case "group.nearby":
    case "group.invited":
      return data.spaceId ? `/group?id=${encodeURIComponent(data.spaceId)}` : "/notifications";
    default:
      return "/notifications";
  }
}
