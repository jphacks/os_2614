"use client";

import { useCallback } from "react";
import { postJson } from "@/data/api";
import type { Notification as NotificationDTO } from "@/data/dto";
import { usePagedResource, type ResourceState } from "@/data/hooks";
import { toNotification } from "@/data/mappers";
import type { Notification } from "@/domain/types";
import { useRealtimeEvent } from "@/realtime/provider";
import { NotificationsScreen } from "@/components/screens";

export default function NotificationsPage() {
  const { state, items, hasMore, loadingMore, loadMore, reload } =
    usePagedResource<NotificationDTO>("/notifications", []);

  useRealtimeEvent("notification", () => reload());

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
    state.status === "ok" ? { status: "ok", data: items.map(toNotification) } : state;

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
