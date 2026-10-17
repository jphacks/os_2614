"use client";

import { useSyncExternalStore } from "react";

type ClockStore = {
  subscribe: (callback: () => void) => () => void;
  getSnapshot: () => number;
  getServerSnapshot: () => number;
};

const stores = new Map<number, ClockStore>();

function getStore(intervalMs: number): ClockStore {
  const cached = stores.get(intervalMs);
  if (cached) {
    return cached;
  }

  const store: ClockStore = {
    subscribe(callback) {
      const id = setInterval(callback, intervalMs);
      return () => clearInterval(id);
    },
    getSnapshot() {
      return Math.floor(Date.now() / intervalMs);
    },
    getServerSnapshot() {
      return 0;
    },
  };
  stores.set(intervalMs, store);
  return store;
}

/**
 * 現在時刻 (ミリ秒)。intervalMs ごとに更新される。
 * サーバ側では 0 を返すので、0 は「未確定」として扱うこと。
 */
export function useNow(intervalMs: number): number {
  const store = getStore(intervalMs);
  const tick = useSyncExternalStore(
    store.subscribe,
    store.getSnapshot,
    store.getServerSnapshot,
  );
  return tick * intervalMs;
}
