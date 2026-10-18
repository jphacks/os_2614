"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { destinationOf } from "@/device/push/PushBootstrap";
import { useRealtimeEvent } from "@/realtime/provider";
import { Icon } from "@/components/ui/Icon";
import styles from "./NotificationToast.module.css";

const VISIBLE_MS = 5000;

const TITLES: Record<string, string> = {
  "friend.request": "フレンド申請",
  "friend.accepted": "フレンド承認",
  "party.confirmed": "パーティ",
  "group.nearby": "近くのグループ",
  "group.invited": "グループへの招待",
  "group.suggested": "グループの提案",
  "post.new": "新しい投稿",
  "moderation.hidden": "投稿のお知らせ",
};

type Toast = { id: string; title: string; body: string; href: string };

/**
 * 前面にいるときに届いた WebSocket の `notification` をトーストで知らせる（12 §1）。
 * 背面・終了中は FCM が OS 通知を出すので、ここは前面専用。通知一覧を開いている間は出さない。
 */
export function NotificationToast() {
  const router = useRouter();
  const pathname = usePathname();
  const [toast, setToast] = useState<Toast | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const dismiss = useCallback(() => {
    if (timer.current) {
      clearTimeout(timer.current);
      timer.current = null;
    }
    setToast(null);
  }, []);

  useEffect(() => dismiss, [dismiss]);

  useRealtimeEvent("notification", (n) => {
    if (pathname === "/notifications" || n.type.startsWith("encounter.")) {
      return;
    }
    const data = (n.data ?? {}) as Record<string, unknown>;
    const spaceId = typeof data.spaceId === "string" ? data.spaceId : "";
    dismiss();
    setToast({
      id: n.id,
      title: TITLES[n.type] ?? "CHAIN",
      body: typeof data.text === "string" ? data.text : "",
      href: destinationOf({ type: n.type, spaceId }),
    });
    timer.current = setTimeout(dismiss, VISIBLE_MS);
  });

  if (!toast) {
    return null;
  }
  return (
    <button
      key={toast.id}
      type="button"
      className={styles.toast}
      role="status"
      onClick={() => {
        const href = toast.href;
        dismiss();
        router.push(href);
      }}
    >
      <Icon name="notifications" size={20} />
      <span className={styles.text}>
        <span className={styles.title}>{toast.title}</span>
        {toast.body && <span className={styles.body}>{toast.body}</span>}
      </span>
    </button>
  );
}
