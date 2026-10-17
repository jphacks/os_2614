"use client";

import { useCallback } from "react";
import { postJson } from "@/data/api";
import type { Notification as NotificationDTO } from "@/data/dto";
import { usePagedResource, type ResourceState } from "@/data/hooks";
import { toNotification } from "@/data/mappers";
import type { Notification } from "@/domain/types";
import { useRealtimeResync } from "@/realtime/provider";
import { useRealtimeCache } from "@/realtime/store";
import { NotificationsScreen } from "@/components/screens";

/** リアルタイムで届いた通知を先頭側に重ね、id が同じものは二重に出さない。 */
function mergeNotifications(
  realtime: Notification[],
  rest: Notification[],
): Notification[] {
  const seen = new Set(realtime.map((item) => item.id));
  return [...realtime, ...rest.filter((item) => !seen.has(item.id))];
}

export default function NotificationsPage() {
  const { state, items, hasMore, loadingMore, loadMore, reload } =
    usePagedResource<NotificationDTO>("/notifications", []);
  const cache = useRealtimeCache();

  // 再接続後は at-most-once の取りこぼしを補うため REST を取り直す (10 §7.3)。
  useRealtimeResync(reload);

  const markRead = useCallback(
    async (ids: string[]) => {
      await postJson<{ updated: number }>("/notifications/read", { ids });
      reload();
    },
    [reload],
  );

  const markAllRead = useCallback(async () => {
    await postJson<{ updated: number }>("/notifications/read", { all: true });
    reload();
  }, [reload]);

  const notificationsState: ResourceState<Notification[]> =
    state.status === "ok"
      ? {
          status: "ok",
          data: mergeNotifications(
            cache.notifications.map(toNotification),
            items.map(toNotification),
          ),
        }
      : state;

  return (
    <NotificationsScreen
      state={notificationsState}
      hasMore={hasMore}
      loadingMore={loadingMore}
      onLoadMore={loadMore}
      onRetry={reload}
      onMarkRead={markRead}
      onMarkAllRead={markAllRead}
    />
  );
}
