import { expect, test } from "@playwright/test";

import {
  buildAuthFrame,
  buildSubFrame,
  buildUnsubFrame,
  buildWsUrl,
  computeBackoff,
  computeReauthDelayMs,
  isAuthError,
  jwtTtlMs,
  parseFrame,
  RealtimeClient,
  type CloseCause,
  type EventFrame,
  type RealtimeState,
  type WebSocketFactory,
  type WebSocketLike,
} from "../src/realtime/ws";

// 純関数 + トランスポートをスタブした状態機械のテスト。ブラウザも実サーバも使わない。
// 実 WebSocket での疎通はやらない (ws ライブラリは puppeteer-core の推移的依存でしかなく、
// pnpm では直接 import できないため)。フレームの bytes・URL の正しさは純関数テストで、
// auth→ready / 切断→再接続 はスタブ WebSocket 越しに実クライアントを回して検証する。

const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

async function waitFor(predicate: () => boolean, timeoutMs = 1_000): Promise<void> {
  const start = Date.now();
  while (!predicate()) {
    if (Date.now() - start > timeoutMs) {
      throw new Error("waitFor timeout");
    }
    await tick();
  }
}

class FakeWebSocket implements WebSocketLike {
  readonly url: string;
  sent: string[] = [];
  closed = false;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: ((event: { code?: number }) => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(url: string) {
    this.url = url;
  }

  send(data: string): void {
    this.sent.push(data);
  }

  close(): void {
    this.closed = true;
  }

  // テスト用ヘルパー。実ソケットのライフサイクルを駆動する。
  open(): void {
    this.onopen?.();
  }

  receive(data: string): void {
    this.onmessage?.({ data });
  }

  drop(): void {
    this.closed = true;
    this.onclose?.({});
  }
}

function makeClient(overrides: Partial<ConstructorParameters<typeof RealtimeClient>[0]> = {}) {
  const sockets: FakeWebSocket[] = [];
  const factory: WebSocketFactory = (url) => {
    const socket = new FakeWebSocket(url);
    sockets.push(socket);
    return socket;
  };
  const client = new RealtimeClient({
    apiBase: "https://chain.example.com",
    getToken: async () => "token-1",
    factory,
    backoffBaseMs: 10,
    backoffMaxMs: 20,
    authTimeoutMs: 200,
    ...overrides,
  });
  return { client, sockets };
}

test.describe("buildWsUrl", () => {
  test("maps https -> wss and appends /api/v1/ws", () => {
    expect(buildWsUrl("https://chain.i32.jp")).toBe("wss://chain.i32.jp/api/v1/ws");
  });

  test("maps http -> ws", () => {
    expect(buildWsUrl("http://localhost:8080")).toBe("ws://localhost:8080/api/v1/ws");
  });

  test("strips a trailing slash", () => {
    expect(buildWsUrl("https://chain.example.com/")).toBe(
      "wss://chain.example.com/api/v1/ws",
    );
  });
});

test.describe("frame builders", () => {
  test("auth frame carries the token in the body", () => {
    expect(JSON.parse(buildAuthFrame("secret"))).toEqual({ type: "auth", token: "secret" });
  });

  test("sub frame carries id and space", () => {
    expect(JSON.parse(buildSubFrame("c1", "space-1"))).toEqual({
      type: "sub",
      id: "c1",
      space: "space-1",
    });
  });

  test("unsub frame carries space only", () => {
    expect(JSON.parse(buildUnsubFrame("space-1"))).toEqual({ type: "unsub", space: "space-1" });
  });
});

test.describe("parseFrame", () => {
  test("parses ready", () => {
    expect(parseFrame('{"type":"ready","did":"did:plc:x"}')).toEqual({
      type: "ready",
      did: "did:plc:x",
    });
  });

  test("parses subscribed", () => {
    expect(parseFrame('{"type":"subscribed","id":"c1","space":"s"}')).toEqual({
      type: "subscribed",
      id: "c1",
      space: "s",
    });
  });

  test("parses unsubscribed with and without reason", () => {
    expect(parseFrame('{"type":"unsubscribed","space":"s","reason":"access_lost"}')).toEqual({
      type: "unsubscribed",
      space: "s",
      reason: "access_lost",
    });
    expect(parseFrame('{"type":"unsubscribed","space":"s"}')).toEqual({
      type: "unsubscribed",
      space: "s",
    });
  });

  test("parses error with and without id", () => {
    expect(parseFrame('{"type":"error","id":"c1","code":"forbidden"}')).toEqual({
      type: "error",
      id: "c1",
      code: "forbidden",
    });
    expect(parseFrame('{"type":"error","code":"unauthenticated"}')).toEqual({
      type: "error",
      code: "unauthenticated",
    });
  });

  test("parses event", () => {
    expect(parseFrame('{"type":"event","event":"post.created","space":"s","data":{"id":1}}')).toEqual(
      { type: "event", event: "post.created", space: "s", data: { id: 1 } },
    );
  });

  test("rejects malformed and unknown frames", () => {
    expect(parseFrame("not json")).toBeNull();
    expect(parseFrame("{}")).toBeNull();
    expect(parseFrame('{"type":"nope"}')).toBeNull();
    expect(parseFrame('"string"')).toBeNull();
  });
});

test.describe("computeBackoff", () => {
  test("grows exponentially and caps at max", () => {
    const r1 = () => 1;
    expect(computeBackoff(0, 1000, 30_000, r1)).toBe(1000);
    expect(computeBackoff(1, 1000, 30_000, r1)).toBe(2000);
    expect(computeBackoff(2, 1000, 30_000, r1)).toBe(4000);
    // 2^5 * 1000 = 32000 > 30000 で打ち切り
    expect(computeBackoff(5, 1000, 30_000, r1)).toBe(30_000);
  });

  test("applies jitter in [0.5, 1.0]", () => {
    expect(computeBackoff(0, 1000, 30_000, () => 0)).toBe(500);
    expect(computeBackoff(0, 1000, 30_000, () => 1)).toBe(1000);
  });
});

test.describe("auth helpers", () => {
  test("isAuthError only for unauthenticated", () => {
    expect(isAuthError("unauthenticated")).toBe(true);
    expect(isAuthError("forbidden")).toBe(false);
    expect(isAuthError("internal")).toBe(false);
  });

  test("jwtTtlMs decodes exp - iat", () => {
    const header = btoa(JSON.stringify({ alg: "none" }));
    const payload = btoa(JSON.stringify({ iat: 100, exp: 200 }));
    expect(jwtTtlMs(`${header}.${payload}.`)).toBe(100_000);
    expect(jwtTtlMs("garbage")).toBeNull();
  });

  test("computeReauthDelayMs subtracts margin and floors at MIN", () => {
    expect(computeReauthDelayMs(10_000, 1_000)).toBe(9_000);
    expect(computeReauthDelayMs(10_000, 9_500)).toBe(1_000); // floor
    expect(computeReauthDelayMs(null, 1_000)).toBe(5 * 60_000); // fallback
  });
});

test.describe("RealtimeClient", () => {
  test("auth→ready: sends auth as the first message, never in the URL", async () => {
    const { client, sockets } = makeClient();
    const states: RealtimeState[] = [];
    client.on("state", (s) => states.push(s));
    const readyEvents: string[] = [];
    client.on("ready", (e) => readyEvents.push(e.did));

    client.connect();
    expect(sockets).toHaveLength(1);
    const socket = sockets[0];
    expect(socket.url).toBe("wss://chain.example.com/api/v1/ws");
    expect(socket.url).not.toContain("token-1");

    socket.open();
    await tick();
    expect(socket.sent).toHaveLength(1);
    expect(JSON.parse(socket.sent[0])).toEqual({ type: "auth", token: "token-1" });

    socket.receive(JSON.stringify({ type: "ready", did: "did:plc:abc" }));
    expect(client.state).toBe("ready");
    expect(readyEvents).toEqual(["did:plc:abc"]);
    expect(states).toContain("connecting");
    expect(states).toContain("authing");
    expect(states).toContain("ready");

    client.close();
  });

  test("close→reconnect: resends auth then subscriptions after a network drop", async () => {
    const { client, sockets } = makeClient();
    client.connect();
    const first = sockets[0];
    first.open();
    await tick();
    first.receive(JSON.stringify({ type: "ready", did: "did:plc:abc" }));

    client.subscribe("space-1");
    expect(first.sent).toContain(JSON.stringify({ type: "sub", id: "c1", space: "space-1" }));

    // ネットワーク断を模す。
    first.drop();
    await waitFor(() => sockets.length === 2);
    const second = sockets[1];
    expect(second.url).toBe("wss://chain.example.com/api/v1/ws");
    second.open();
    await tick();
    expect(second.sent).toHaveLength(1);
    expect(JSON.parse(second.sent[0])).toEqual({ type: "auth", token: "token-1" });

    // ready が来てから購読を再送する。
    second.receive(JSON.stringify({ type: "ready", did: "did:plc:abc" }));
    expect(second.sent.slice(1).map((m) => JSON.parse(m))).toEqual([
      { type: "sub", id: "c2", space: "space-1" },
    ]);

    client.close();
  });

  test("subscription error removes the space and reports it", async () => {
    const { client, sockets } = makeClient();
    const errors: { id?: string; space?: string; code: string }[] = [];
    client.on("error", (e) => errors.push(e));
    client.connect();
    const socket = sockets[0];
    socket.open();
    await tick();
    socket.receive(JSON.stringify({ type: "ready", did: "d" }));
    client.subscribe("space-x");
    socket.receive(JSON.stringify({ type: "error", id: "c1", code: "forbidden" }));
    expect(errors).toEqual([{ id: "c1", space: "space-x", code: "forbidden" }]);
    expect(client.subscribedSpaces).toEqual([]);
    client.close();
  });

  test("auth error is terminal (no reconnect loop)", async () => {
    const { client, sockets } = makeClient();
    const closes: { cause: CloseCause }[] = [];
    client.on("close", (e) => closes.push(e));
    client.connect();
    const socket = sockets[0];
    socket.open();
    await tick();
    socket.receive(JSON.stringify({ type: "error", code: "unauthenticated" }));
    socket.drop();
    await tick();
    expect(sockets).toHaveLength(1);
    expect(client.state).toBe("closed");
    expect(closes).toEqual([{ cause: "auth" }]);
  });

  test("enforces the 50-space subscription cap", () => {
    const { client } = makeClient();
    client.connect();
    for (let i = 0; i < 50; i += 1) {
      client.subscribe(`space-${i}`);
    }
    expect(() => client.subscribe("space-50")).toThrow("subscription limit reached (50)");
    // 既存 space の再購読は同じ id を返す。
    expect(client.subscribe("space-0")).toBe("c1");
    client.close();
  });

  test("forwards event frames and server-driven unsubscribes", async () => {
    const { client, sockets } = makeClient();
    const events: EventFrame[] = [];
    client.on("event", (e) => events.push(e));
    client.connect();
    const socket = sockets[0];
    socket.open();
    await tick();
    socket.receive(JSON.stringify({ type: "ready", did: "d" }));
    client.subscribe("space-1");
    socket.receive(
      JSON.stringify({ type: "event", event: "post.created", space: "space-1", data: { id: 1 } }),
    );
    expect(events).toEqual([
      { type: "event", event: "post.created", space: "space-1", data: { id: 1 } },
    ]);

    socket.receive(
      JSON.stringify({ type: "unsubscribed", space: "space-1", reason: "access_lost" }),
    );
    expect(client.subscribedSpaces).toEqual([]);
    client.close();
  });
});
