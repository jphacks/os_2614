"use client";

// リアルタイム (WebSocket) を signedIn の間だけ 1 本だけ張る provider (10 §7)。
//
// ws.ts (todo 27) が接続・購読・再接続を、events.ts (todo 28) がフレームの振り分けと
// Space 購読 / REST 再取得の seam を持つ。ここはその 2 つを React のライフサイクルに
// つなぐだけ:
// - signedIn のときだけ接続し、signedOut / アンマウントで閉じる (残接続しない)。
// - アプリ全体で 1 つだけ置く (layout)。画面は接続を持たず、context 越しに使う。
// - 表示中画面は useRealtimeSubscription(spaceId) で購読を登録 / 解除する。
// - 再接続後は useRealtimeResync(handler) で対象 REST を取り直す (10 §7.3)。
// - 画面は useRealtimeEvent(type, handler) でイベント種別ごとに反応する (events.ts)。
//
// effect 本体で同期 setState しないため (react-hooks/set-state-in-effect)、接続は
// 外部ストアとして持ち、router を useSyncExternalStore で読む。

import {
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";

import { useAuth } from "@/auth";
import { RealtimeEventRouter, RealtimeEventsContext, useRealtimeRouter } from "./events";
import { RealtimeClient } from "./ws";

export { useRealtimeEvent } from "./events";

/**
 * 停止を 1 tick 遅らせる。React StrictMode は開発時に effect を
 * mount → cleanup → mount と二重に走らせる。cleanup で即 close すると、直後の
 * remount が終端 (closed) の client を再利用して接続が復帰しなくなるため、
 * 同じ tick 内で start が来たら停止を取り消す (疑似 unmount と本物の unmount を区別する)。
 */
const TEARDOWN_DELAY_MS = 0;

/**
 * 1 本の接続と router を持つ。start / stop を繰り返しても毎回新しい RealtimeClient を
 * 作るので、終端になった client を再利用しない。
 */
class RealtimeConnection {
  private client: RealtimeClient | null = null;
  private detach: (() => void) | null = null;
  private snapshot: RealtimeEventRouter | null = null;
  private pendingStop: ReturnType<typeof setTimeout> | null = null;
  private readonly listeners = new Set<() => void>();

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getSnapshot = (): RealtimeEventRouter | null => this.snapshot;

  /** 接続を開始する。二重 start は無視し、保留中の停止は取り消す。 */
  start(): void {
    if (this.pendingStop !== null) {
      clearTimeout(this.pendingStop);
      this.pendingStop = null;
    }
    if (this.client !== null) {
      return;
    }
    const client = new RealtimeClient();
    const router = new RealtimeEventRouter(client);
    this.client = client;
    this.detach = router.attach();
    this.snapshot = router;
    this.emit();
    client.connect();
  }

  /** 接続を止める (1 tick 後)。同じ tick で start が来たら取り消される。 */
  stop(): void {
    if (this.client === null || this.pendingStop !== null) {
      return;
    }
    this.pendingStop = setTimeout(() => {
      this.pendingStop = null;
      this.closeNow();
    }, TEARDOWN_DELAY_MS);
  }

  private closeNow(): void {
    const client = this.client;
    if (client === null) {
      return;
    }
    this.detach?.();
    this.detach = null;
    this.client = null;
    this.snapshot = null;
    this.emit();
    client.close();
  }

  private emit(): void {
    for (const listener of this.listeners) {
      listener();
    }
  }
}

/**
 * 認証済みのときだけリアルタイム接続を張る。未ログインでは接続を作らず、context を
 * null にして画面の hook を no-op にする。
 */
export function RealtimeProvider({ children }: { children: ReactNode }) {
  const { state } = useAuth();
  if (state.status !== "signedIn") {
    return (
      <RealtimeEventsContext.Provider value={null}>{children}</RealtimeEventsContext.Provider>
    );
  }
  return <RealtimeConnectedProvider>{children}</RealtimeConnectedProvider>;
}

function RealtimeConnectedProvider({ children }: { children: ReactNode }) {
  const [connection] = useState(() => new RealtimeConnection());
  const router = useSyncExternalStore(
    connection.subscribe,
    connection.getSnapshot,
    connection.getSnapshot,
  );

  useEffect(() => {
    connection.start();
    return () => connection.stop();
  }, [connection]);

  return <RealtimeEventsContext.Provider value={router}>{children}</RealtimeEventsContext.Provider>;
}

/**
 * 表示中の画面が Space を購読する。アンマウント / spaceId 変更で解除する。
 * provider 未設置 (未ログイン) や空文字のときは何もしない。
 */
export function useRealtimeSubscription(spaceId: string | null): void {
  const router = useRealtimeRouter();
  useEffect(() => {
    if (router === null || spaceId === null || spaceId === "") {
      return;
    }
    try {
      router.subscribe(spaceId);
    } catch {
      // 購読上限 (50) などで購読できない場合は、その画面のリアルタイム更新だけ諦める。
      return;
    }
    return () => router.unsubscribe(spaceId);
  }, [router, spaceId]);
}

/**
 * 再接続後に走らせる REST 再取得を登録する (10 §7.3: at-most-once の取りこぼし補填)。
 * handler は常に最新を ref 経由で呼ぶので、再登録は起きない。
 */
export function useRealtimeResync(handler: () => void): void {
  const router = useRealtimeRouter();
  const latest = useRef(handler);
  useEffect(() => {
    latest.current = handler;
  });
  useEffect(() => {
    if (router === null) {
      return;
    }
    return router.onResync(() => latest.current());
  }, [router]);
}
