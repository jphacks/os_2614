import { expect, test } from "@playwright/test";

import type { Sighting } from "../src/device/proximity/definitions";
import { SightingOutbox } from "../src/device/proximity/sync";
import {
  SIGHTING_UPLOAD_INTERVAL_MS,
  drainIntoSightingsOutbox,
  flushSightings,
  sightingFlushReason,
  syncSightings,
  type SightingSource,
} from "../src/device/proximity/sightings";

// outbox → POST /encounters/sightings の at-least-once 送信を検証する。ブラウザは開かず、
// global fetch と localStorage だけをスタブして実モジュール (sightings.ts → data/api → auth/api)
// を通す。token は不透明な文字列として扱う。

const SESSION_KEY = "chain.auth.session.v1";

function installSession(): void {
  const map = new Map<string, string>();
  const storage: Storage = {
    get length() {
      return map.size;
    },
    clear: () => map.clear(),
    getItem: (key) => map.get(key) ?? null,
    key: (index) => Array.from(map.keys())[index] ?? null,
    removeItem: (key) => void map.delete(key),
    setItem: (key, value) => void map.set(key, value),
  };
  (globalThis as { localStorage: Storage }).localStorage = storage;
  (globalThis as { window: Window }).window = {
    addEventListener: () => {},
    removeEventListener: () => {},
  } as unknown as Window;
  storage.setItem(
    SESSION_KEY,
    JSON.stringify({
      did: "did:plc:testuser",
      accessToken: "test-access-token",
      refreshToken: "test-refresh-token",
      accessExpiresAtMs: Date.now() + 60 * 60 * 1000,
    }),
  );
}

installSession();

type FetchHandler = (url: string, init?: RequestInit) => Response | Promise<Response>;

let handler: FetchHandler = () => json(500, { error: { code: "internal", message: "no handler" } });

globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
  const url =
    typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
  return Promise.resolve(handler(url, init));
}) as typeof fetch;

function json(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", ...headers },
  });
}

let storageSeq = 0;

function freshOutbox(): SightingOutbox {
  return new SightingOutbox(`chain.proximity.sightings.spec.${storageSeq++}`);
}

function sighting(token: string, lastSeenAt: string, firstSeenAt: string = lastSeenAt): Sighting {
  return { token, firstSeenAt, lastSeenAt, maxRssi: -60, count: 1 };
}

function iso(ms: number): string {
  return new Date(ms).toISOString();
}

test.describe("flushSightings", () => {
  test("POSTs the outbox batch and acks it on success", async () => {
    const outbox = freshOutbox();
    const now = Date.now();
    outbox.add([sighting("token-a", iso(now - 60_000))]);

    const requests: { url: string; method: string; auth: string | null; body: unknown }[] = [];
    handler = (url, init) => {
      requests.push({
        url,
        method: init?.method ?? "GET",
        auth: new Headers(init?.headers).get("Authorization"),
        body: init?.body ? JSON.parse(String(init.body)) : null,
      });
      return json(200, { accepted: 1, dropped: 0 });
    };

    const result = await flushSightings({ outbox });

    expect(requests).toHaveLength(1);
    expect(requests[0].url).toContain("/api/v1/encounters/sightings");
    expect(requests[0].method).toBe("POST");
    expect(requests[0].auth).toBe("Bearer test-access-token");
    expect(requests[0].body).toEqual({ items: [sighting("token-a", iso(now - 60_000))] });
    expect(result).toMatchObject({
      sent: 1,
      batches: 1,
      pending: 0,
      stopped: "empty",
      retryAfterMs: null,
    });
    expect(outbox.size).toBe(0);
  });

  test("keeps items queued on failure and resends them on the next flush", async () => {
    const outbox = freshOutbox();
    const now = Date.now();
    outbox.add([sighting("token-a", iso(now)), sighting("token-b", iso(now))]);

    let calls = 0;
    handler = () => {
      calls += 1;
      return calls === 1
        ? json(500, { error: { code: "internal", message: "boom" } })
        : json(200, { accepted: 2, dropped: 0 });
    };

    const first = await flushSightings({ outbox });
    expect(first.stopped).toBe("error");
    expect(first.sent).toBe(0);
    expect(first.pending).toBe(2);
    expect(outbox.size).toBe(2);

    const second = await flushSightings({ outbox });
    expect(second.stopped).toBe("empty");
    expect(second.sent).toBe(2);
    expect(outbox.size).toBe(0);
  });

  test("backs off on 429 and reports the Retry-After in ms", async () => {
    const outbox = freshOutbox();
    outbox.add([sighting("token-a", iso(Date.now()))]);

    handler = () =>
      json(429, { error: { code: "rate_limited", message: "too many" } }, { "Retry-After": "30" });

    const result = await flushSightings({ outbox });

    expect(result.stopped).toBe("rate_limited");
    expect(result.retryAfterMs).toBe(30_000);
    expect(result.sent).toBe(0);
    expect(result.pending).toBe(1);
    expect(outbox.size).toBe(1);
  });

  test("falls back to the bucket window when 429 has no Retry-After", async () => {
    const outbox = freshOutbox();
    outbox.add([sighting("token-a", iso(Date.now()))]);

    handler = () => json(429, { error: { code: "rate_limited", message: "too many" } });

    const result = await flushSightings({ outbox });

    expect(result.stopped).toBe("rate_limited");
    expect(result.retryAfterMs).toBe(60_000);
  });

  test("splits more than 500 items into 500-capped requests", async () => {
    const outbox = freshOutbox();
    const seenAt = iso(Date.now());
    outbox.add(Array.from({ length: 1200 }, (_, i) => sighting(`token-${i}`, seenAt)));

    const sizes: number[] = [];
    handler = (_url, init) => {
      const body = JSON.parse(String(init?.body)) as { items: Sighting[] };
      sizes.push(body.items.length);
      return json(200, { accepted: body.items.length, dropped: 0 });
    };

    const result = await flushSightings({ outbox });

    expect(sizes).toEqual([500, 500, 200]);
    expect(result.batches).toBe(3);
    expect(result.sent).toBe(1200);
    expect(result.pending).toBe(0);
    expect(outbox.size).toBe(0);
  });

  test("never sends an item that aged past 24h between flushes", async () => {
    const outbox = freshOutbox();
    const t0 = Date.parse("2030-01-01T00:00:00Z");
    outbox.add([sighting("old", iso(t0))], t0);

    let requests = 0;
    handler = () => {
      requests += 1;
      return json(200, { accepted: 1, dropped: 0 });
    };

    const result = await flushSightings({ outbox, nowMs: () => t0 + 25 * 3_600_000 });

    expect(requests).toBe(0);
    expect(result.stopped).toBe("empty");
    expect(result.sent).toBe(0);
  });
});

test.describe("sightingFlushReason", () => {
  const base = {
    pending: 10,
    running: true,
    nowMs: 1_000_000,
    lastFlushAtMs: 1_000_000 - SIGHTING_UPLOAD_INTERVAL_MS,
  };

  test("null when nothing is pending", () => {
    expect(sightingFlushReason({ ...base, pending: 0 })).toBeNull();
  });

  test("pending only when more than 50 are queued, even while not running", () => {
    expect(sightingFlushReason({ ...base, pending: 51, running: false })).toBe("pending");
    expect(sightingFlushReason({ ...base, pending: 50, running: false })).toBeNull();
  });

  test("interval after 5 minutes only while the FGS is running", () => {
    expect(sightingFlushReason(base)).toBe("interval");
    expect(sightingFlushReason({ ...base, lastFlushAtMs: base.nowMs - 60_000 })).toBeNull();
    expect(sightingFlushReason({ ...base, running: false })).toBeNull();
  });

  test("foreground wins while something is pending", () => {
    expect(
      sightingFlushReason({ ...base, foreground: true, lastFlushAtMs: base.nowMs }),
    ).toBe("foreground");
  });

  test("backoff suppresses every trigger until it expires", () => {
    const input = { ...base, foreground: true, rateLimitedUntilMs: base.nowMs + 1 };
    expect(sightingFlushReason(input)).toBeNull();
    expect(sightingFlushReason({ ...input, rateLimitedUntilMs: base.nowMs })).toBe("foreground");
  });
});

test.describe("drain and sync", () => {
  test("drains native sightings into the outbox", async () => {
    const outbox = freshOutbox();
    const now = Date.now();
    const items = [sighting("token-a", iso(now)), sighting("token-b", iso(now))];
    const source: SightingSource = { drainSightings: async () => ({ items }) };

    const drained = await drainIntoSightingsOutbox(source, outbox, now);

    expect(drained).toBe(2);
    expect(outbox.size).toBe(2);
  });

  test("drains then flushes when pending exceeds 50", async () => {
    const outbox = freshOutbox();
    const now = Date.now();
    const items = Array.from({ length: 51 }, (_, i) => sighting(`t-${i}`, iso(now)));
    const source: SightingSource = { drainSightings: async () => ({ items }) };

    const sizes: number[] = [];
    handler = (_url, init) => {
      const body = JSON.parse(String(init?.body)) as { items: Sighting[] };
      sizes.push(body.items.length);
      return json(200, { accepted: body.items.length, dropped: 0 });
    };

    const result = await syncSightings({ outbox, source, running: false, nowMs: () => now });

    expect(result.drained).toBe(51);
    expect(result.reason).toBe("pending");
    expect(result.flush?.sent).toBe(51);
    expect(sizes).toEqual([51]);
    expect(outbox.size).toBe(0);
  });

  test("drains but does not send when no trigger applies", async () => {
    const outbox = freshOutbox();
    const now = Date.now();
    const items = [sighting("token-a", iso(now))];
    const source: SightingSource = { drainSightings: async () => ({ items }) };

    let requests = 0;
    handler = () => {
      requests += 1;
      return json(200, { accepted: 1, dropped: 0 });
    };

    const result = await syncSightings({
      outbox,
      source,
      running: false,
      nowMs: () => now,
    });

    expect(result.drained).toBe(1);
    expect(result.reason).toBeNull();
    expect(result.flush).toBeNull();
    expect(requests).toBe(0);
    expect(outbox.size).toBe(1);
  });
});
