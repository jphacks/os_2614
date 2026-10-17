import { expect, test } from "@playwright/test";

import {
  isKnownEvent,
  REALTIME_EVENT_NAMES,
  RealtimeEventRouter,
  type RealtimeEventDataMap,
  type RealtimeEventName,
  type RealtimeTransport,
} from "../src/realtime/events";
import { parseFrame, type EventFrame, type RealtimeEvents, type RealtimeState } from "../src/realtime/ws";
import { RealtimeStore, attachRealtimeStore } from "../src/realtime/store";

// イベント振り分け (src/realtime/events.ts) のテスト。ブラウザも実サーバも使わない。
// ws.ts のトランスポートは軽いスタブで置き換え、ルーターが
// - 10 §7.3 の全イベントを該当購読者へ配ること
// - 未知のイベントを無視して落ちないこと
// - Space 購読を transport へ委譲すること
// - 再接続後にだけ REST 再取得 (resync) を起こすこと
// を検証する。hook (useRealtimeEvent) は React レンダラが要るのでここでは回さない。

/** RealtimeEvents を型付きで発火できる小さな transport。 */
class StubTransport implements RealtimeTransport {
  private readonly listeners = new Map<keyof RealtimeEvents, Set<(payload: unknown) => void>>();
  private readonly spaces = new Map<string, string>();
  private counter = 0;

  on<K extends keyof RealtimeEvents>(
    event: K,
    handler: (payload: RealtimeEvents[K]) => void,
  ): () => void {
    let set = this.listeners.get(event);
    if (set === undefined) {
      set = new Set();
      this.listeners.set(event, set);
    }
    const wrapped = (payload: unknown) => handler(payload as RealtimeEvents[K]);
    set.add(wrapped);
    return () => {
      set.delete(wrapped);
    };
  }

  subscribe(space: string): string {
    const existing = this.spaces.get(space);
    if (existing !== undefined) {
      return existing;
    }
    this.counter += 1;
    const id = `c${this.counter}`;
    this.spaces.set(space, id);
    return id;
  }

  unsubscribe(space: string): void {
    this.spaces.delete(space);
  }

  get subscribedSpaces(): string[] {
    return Array.from(this.spaces.keys());
  }

  /** テストから transport の発火を模す。 */
  emit<K extends keyof RealtimeEvents>(event: K, payload: RealtimeEvents[K]): void {
    const set = this.listeners.get(event);
    if (set === undefined) {
      return;
    }
    for (const handler of set) {
      handler(payload);
    }
  }
}

/** 各イベントの代表的な data (backend が publish するワイヤ上の形)。 */
const SAMPLE: { [K in RealtimeEventName]: RealtimeEventDataMap[K] } = {
  "post.created": {
    kind: "post",
    uri: "at://did:plc:author/jp.i32.chain.post/1",
    spaceId: "space-1",
    text: "hello",
    createdAt: "2026-10-17T00:00:00Z",
  },
  "post.deleted": { uri: "at://did:plc:author/jp.i32.chain.post/1" },
  "media.created": {
    id: "media-1",
    alt: "alt",
    url: "https://cdn.example.com/1.jpg",
    thumbUrl: "https://cdn.example.com/1.thumb.jpg",
    width: 100,
    height: 200,
  },
  "pin.created": {
    uri: "at://did:plc:auth/jp.i32.chain.instance.pin/1",
    subject: "at://did:plc:author/jp.i32.chain.post/1",
    reason: "auto",
    createdAt: "2026-10-17T00:00:00Z",
  },
  "pin.deleted": { uri: "at://did:plc:auth/jp.i32.chain.instance.pin/1" },
  "member.joined": { did: "did:plc:peer" },
  "member.left": { did: "did:plc:peer" },
  "space.archived": undefined,
	"party.updated": { id: "party-1", phase: "confirmed", locked: true, present: ["did:plc:peer"] },
  "encounter.revealed": {
    id: "enc-1",
    peer: { did: "did:plc:peer", handle: "peer.example.com" },
    seenFrom: "2026-10-17T00:00:00Z",
    seenUntil: "2026-10-17T00:15:00Z",
    timesWithPeer: 2,
    relation: "none",
  },
  "friend.request": { did: "did:plc:peer", handle: "peer.example.com" },
  "friend.accepted": { did: "did:plc:peer", handle: "peer.example.com" },
  "friend.changed": { did: "did:plc:peer", handle: "peer.example.com" },
  "notification": {
    id: "notif-1",
    type: "friend.request",
    data: { from: "did:plc:peer" },
    createdAt: "2026-10-17T00:00:00Z",
  },
};

function eventFrame<K extends RealtimeEventName>(
  name: K,
  data: RealtimeEventDataMap[K],
): EventFrame {
  return { type: "event", event: name, space: "space-1", data };
}

test.describe("REALTIME_EVENT_NAMES", () => {
  test("covers the 10 §7.3 table", () => {
    // 表の 14 種別 (pin.* / member.* は展開)。抜け・重複が無いこと。
    expect(REALTIME_EVENT_NAMES).toHaveLength(14);
    expect(new Set(REALTIME_EVENT_NAMES).size).toBe(14);
    for (const name of REALTIME_EVENT_NAMES) {
      expect(isKnownEvent(name)).toBe(true);
    }
  });

  test("isKnownEvent rejects anything outside the table", () => {
    expect(isKnownEvent("post.created")).toBe(true);
    expect(isKnownEvent("friend.request")).toBe(true);
    expect(isKnownEvent("mystery.event")).toBe(false);
    expect(isKnownEvent("")).toBe(false);
    expect(isKnownEvent("POST.CREATED")).toBe(false);
  });
});

test.describe("RealtimeEventRouter.dispatch", () => {
  test("every known event reaches its subscriber", () => {
    const router = new RealtimeEventRouter(new StubTransport());

    for (const name of REALTIME_EVENT_NAMES) {
      const received: unknown[] = [];
      const off = router.on(name, (data) => received.push(data));

      expect(router.dispatch(eventFrame(name, SAMPLE[name]))).toBe(true);
      expect(received).toEqual([SAMPLE[name]]);

      // 解除後は届かない。
      off();
      router.dispatch(eventFrame(name, SAMPLE[name]));
      expect(received).toEqual([SAMPLE[name]]);
    }
  });

  test("an event goes only to subscribers of its own type", () => {
    const router = new RealtimeEventRouter(new StubTransport());
    const created: unknown[] = [];
    const deleted: unknown[] = [];
    router.on("post.created", (data) => created.push(data));
    router.on("post.deleted", (data) => deleted.push(data));

    expect(router.dispatch(eventFrame("post.deleted", SAMPLE["post.deleted"]))).toBe(true);
    expect(created).toEqual([]);
    expect(deleted).toEqual([SAMPLE["post.deleted"]]);
  });

  test("unknown events are ignored safely", () => {
    const router = new RealtimeEventRouter(new StubTransport());
    const received: unknown[] = [];
    router.on("post.created", (data) => received.push(data));

    const unknown: EventFrame = {
      type: "event",
      event: "mystery.event",
      space: "space-1",
      data: { anything: true },
    };
    expect(() => router.dispatch(unknown)).not.toThrow();
    expect(router.dispatch(unknown)).toBe(false);
    expect(received).toEqual([]);
  });

  test("dispatches an event frame produced by ws.parseFrame", () => {
    const router = new RealtimeEventRouter(new StubTransport());
    const received: unknown[] = [];
    router.on("member.joined", (data) => received.push(data));

    const parsed = parseFrame(
      JSON.stringify({ type: "event", event: "member.joined", space: "space-1", data: { did: "did:plc:peer" } }),
    );
    if (parsed === null || parsed.type !== "event") {
      throw new Error("expected an event frame");
    }
    expect(router.dispatch(parsed)).toBe(true);
    expect(received).toEqual([{ did: "did:plc:peer" }]);
  });
});

test.describe("RealtimeEventRouter.attach / subscribe", () => {
  test("attach routes transport frames and detach stops them", () => {
    const transport = new StubTransport();
    const router = new RealtimeEventRouter(transport);
    const received: unknown[] = [];
    router.on("notification", (data) => received.push(data));

    const detach = router.attach();
    transport.emit("event", eventFrame("notification", SAMPLE["notification"]));
    expect(received).toEqual([SAMPLE["notification"]]);

    detach();
    transport.emit("event", eventFrame("notification", SAMPLE["notification"]));
    expect(received).toHaveLength(1);
  });

  test("subscribe / unsubscribe delegate to the transport", () => {
    const transport = new StubTransport();
    const router = new RealtimeEventRouter(transport);

    expect(router.subscribe("space-1")).toBe("c1");
    // 同じ Space は同じ id。50 上限の判定は ws.ts 側 (10 §7.2)。
    expect(router.subscribe("space-1")).toBe("c1");
    expect(router.subscribe("space-2")).toBe("c2");
    expect(router.subscribedSpaces).toEqual(["space-1", "space-2"]);

    router.unsubscribe("space-1");
    expect(router.subscribedSpaces).toEqual(["space-2"]);
  });
});

test.describe("RealtimeEventRouter resync seam", () => {
  test("resync runs after a reconnect, not on the first ready", () => {
    const transport = new StubTransport();
    const router = new RealtimeEventRouter(transport);
    router.attach();
    let resyncs = 0;
    const off = router.onResync(() => {
      resyncs += 1;
    });

    const states: RealtimeState[] = ["connecting", "authing", "ready"];
    for (const state of states) {
      transport.emit("state", state);
    }
    expect(resyncs).toBe(0);

    // 切断 → 再 ready で 1 回だけ取り直す (10 §7.3)。
    transport.emit("state", "reconnecting");
    transport.emit("state", "connecting");
    transport.emit("state", "ready");
    expect(resyncs).toBe(1);

    // 同じ接続で ready が重複しても増えない。
    transport.emit("state", "ready");
    expect(resyncs).toBe(1);

    // 解除したら呼ばれない。
    off();
    transport.emit("state", "reconnecting");
    transport.emit("state", "ready");
    expect(resyncs).toBe(1);
  });

  test("notifyReconnected runs handlers immediately", () => {
    const router = new RealtimeEventRouter(new StubTransport());
    let calls = 0;
    router.onResync(() => {
      calls += 1;
    });
    router.notifyReconnected();
    router.notifyReconnected();
    expect(calls).toBe(2);
  });
});

// レビュー F4 の指摘 (todo 28 の受入: 各イベントを注入し該当キャッシュが更新される)。
// handler が呼ばれたことではなく、キャッシュ (snapshot) の中身が変わったことを検証する。
test.describe("RealtimeStore cache reflection", () => {
  function wired(): { router: RealtimeEventRouter; store: RealtimeStore } {
    const router = new RealtimeEventRouter(new StubTransport());
    const store = new RealtimeStore();
    attachRealtimeStore(router, store);
    return { router, store };
  }

  test("every known event advances the cache revision", () => {
    for (const name of REALTIME_EVENT_NAMES) {
      const { router, store } = wired();
      const before = store.getSnapshot();
      expect(router.dispatch(eventFrame(name, SAMPLE[name]))).toBe(true);
      const after = store.getSnapshot();
      expect(after).not.toBe(before);
      expect(after.revision).toBe(before.revision + 1);
    }
  });

  test("space events land in that space's cache", () => {
    const { router, store } = wired();
    expect(router.dispatch(eventFrame("post.created", SAMPLE["post.created"]))).toBe(true);
    router.dispatch(eventFrame("media.created", SAMPLE["media.created"]));
    router.dispatch(eventFrame("pin.created", SAMPLE["pin.created"]));
    router.dispatch(eventFrame("member.joined", SAMPLE["member.joined"]));

    const space = store.getSnapshot().spaces["space-1"];
    expect(space.posts).toEqual([SAMPLE["post.created"]]);
    expect(space.media).toEqual([SAMPLE["media.created"]]);
    expect(space.pins).toEqual([SAMPLE["pin.created"]]);
    expect(space.joined).toEqual(["did:plc:peer"]);
    expect(space.archived).toBe(false);
  });

  test("post.deleted removes the post and records the uri", () => {
    const { router, store } = wired();
    router.dispatch(eventFrame("post.created", SAMPLE["post.created"]));
    router.dispatch(eventFrame("post.deleted", SAMPLE["post.deleted"]));

    const space = store.getSnapshot().spaces["space-1"];
    expect(space.posts).toEqual([]);
    expect(space.deletedPosts).toEqual([SAMPLE["post.deleted"].uri]);
  });

  test("pin.deleted removes the pin and records the uri", () => {
    const { router, store } = wired();
    router.dispatch(eventFrame("pin.created", SAMPLE["pin.created"]));
    router.dispatch(eventFrame("pin.deleted", SAMPLE["pin.deleted"]));

    const space = store.getSnapshot().spaces["space-1"];
    expect(space.pins).toEqual([]);
    expect(space.deletedPins).toEqual([SAMPLE["pin.deleted"].uri]);
  });

  test("member.left flips the member from joined to left", () => {
    const { router, store } = wired();
    router.dispatch(eventFrame("member.joined", { did: "did:plc:peer" }));
    router.dispatch(eventFrame("member.left", { did: "did:plc:peer" }));

    const space = store.getSnapshot().spaces["space-1"];
    expect(space.joined).toEqual([]);
    expect(space.left).toEqual(["did:plc:peer"]);
  });

  test("space.archived marks the space", () => {
    const { router, store } = wired();
    expect(router.dispatch(eventFrame("space.archived", undefined))).toBe(true);
    expect(store.getSnapshot().spaces["space-1"].archived).toBe(true);
  });

  test("party.updated replaces the cached party", () => {
    const { router, store } = wired();
    router.dispatch(eventFrame("party.updated", SAMPLE["party.updated"]));
    expect(store.getSnapshot().party).toEqual(SAMPLE["party.updated"]);
  });

  test("encounter.revealed is prepended and deduped by id", () => {
    const { router, store } = wired();
    router.dispatch(eventFrame("encounter.revealed", SAMPLE["encounter.revealed"]));
    router.dispatch(eventFrame("encounter.revealed", SAMPLE["encounter.revealed"]));
    expect(store.getSnapshot().encounters).toEqual([SAMPLE["encounter.revealed"]]);
  });

  test("friend events are cached with their kind", () => {
    const { router, store } = wired();
    router.dispatch(eventFrame("friend.request", SAMPLE["friend.request"]));
    router.dispatch(eventFrame("friend.accepted", SAMPLE["friend.accepted"]));
    router.dispatch(eventFrame("friend.changed", SAMPLE["friend.changed"]));

    expect(store.getSnapshot().friendEvents).toEqual([
      { kind: "changed", user: SAMPLE["friend.changed"] },
      { kind: "accepted", user: SAMPLE["friend.accepted"] },
      { kind: "request", user: SAMPLE["friend.request"] },
    ]);
  });

  test("notification is cached newest-first and deduped by id", () => {
    const { router, store } = wired();
    router.dispatch(eventFrame("notification", SAMPLE["notification"]));
    router.dispatch(eventFrame("notification", SAMPLE["notification"]));
    expect(store.getSnapshot().notifications).toEqual([SAMPLE["notification"]]);
  });

  test("unknown events are ignored safely without touching the cache", () => {
    const { router, store } = wired();
    const before = store.getSnapshot();
    const unknown: EventFrame = {
      type: "event",
      event: "mystery.event",
      space: "space-1",
      data: { anything: true },
    };
    expect(router.dispatch(unknown)).toBe(false);
    expect(store.getSnapshot()).toBe(before);
    expect(store.apply("mystery.event", { anything: true })).toBe(false);
  });

  test("subscribers are notified on change, and reset clears the cache", () => {
    const { router, store } = wired();
    let notifications = 0;
    const off = store.subscribe(() => {
      notifications += 1;
    });

    router.dispatch(eventFrame("notification", SAMPLE["notification"]));
    expect(notifications).toBe(1);
    expect(store.getSnapshot().notifications).toHaveLength(1);

    store.reset();
    expect(notifications).toBe(2);
    expect(store.getSnapshot().notifications).toEqual([]);
    expect(store.getSnapshot().revision).toBe(0);

    off();
    router.dispatch(eventFrame("notification", SAMPLE["notification"]));
    expect(notifications).toBe(2);
  });
});
