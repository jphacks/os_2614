import { expect, test } from "@playwright/test";

import {
  fetchBeaconTokens,
  syncBeaconTokens,
  type BeaconSink,
} from "../src/device/proximity/beacon";
import type { BeaconToken } from "../src/device/proximity/definitions";
import {
  estimateClockOffset,
  selectCurrentToken,
  tokensNeedRefresh,
} from "../src/device/proximity/sync";
import { ApiError } from "../src/auth/errors";

// 純関数と、データ層経由の token 取得を検証する。ブラウザは開かず、global fetch と
// localStorage だけをスタブして実モジュール (beacon.ts → data/api → auth/api) を通す。
// token は不透明な文字列として扱う (中身の意味は assert しない)。

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

const TOKEN_A: BeaconToken = { windowStart: "2030-01-01T00:00:00Z", token: "opaque-token-a" };
const TOKEN_B: BeaconToken = { windowStart: "2030-01-01T00:15:00Z", token: "opaque-token-b" };

test.describe("selectCurrentToken", () => {
  test("returns the newest window that contains nowMs", () => {
    const now = Date.parse("2030-01-01T00:20:00Z");
    expect(selectCurrentToken([TOKEN_A, TOKEN_B], 900, now)).toEqual(TOKEN_B);
  });

  test("returns null when no window contains nowMs", () => {
    const now = Date.parse("2030-01-01T05:00:00Z");
    expect(selectCurrentToken([TOKEN_A, TOKEN_B], 900, now)).toBeNull();
  });

  test("skips tokens with an unparsable windowStart", () => {
    const now = Date.parse("2030-01-01T00:05:00Z");
    const broken: BeaconToken = { windowStart: "not-a-date", token: "opaque-broken" };
    expect(selectCurrentToken([broken, TOKEN_A], 900, now)).toEqual(TOKEN_A);
  });
});

test.describe("tokensNeedRefresh (6h threshold)", () => {
  test("true when there are no tokens", () => {
    expect(tokensNeedRefresh([], 900, Date.now())).toBe(true);
  });

  test("false while the latest window ends more than 6h away", () => {
    const now = Date.parse("2029-12-31T00:00:00Z");
    expect(tokensNeedRefresh([TOKEN_A], 900, now, 6)).toBe(false);
  });

  test("true when the latest window ends in less than 6h", () => {
    const now = Date.parse("2030-01-01T00:10:00Z");
    expect(tokensNeedRefresh([TOKEN_A], 900, now, 6)).toBe(true);
  });

  test("true when the latest window has already ended", () => {
    const now = Date.parse("2030-01-02T00:00:00Z");
    expect(tokensNeedRefresh([TOKEN_A], 900, now, 6)).toBe(true);
  });
});

test.describe("estimateClockOffset", () => {
  test("returns 0 for an empty sample set", () => {
    expect(estimateClockOffset([])).toBe(0);
  });

  test("returns the single sample offset (serverDate - midpoint)", () => {
    expect(
      estimateClockOffset([{ sentAtMs: 1_000, receivedAtMs: 2_000, serverDateMs: 5_000 }]),
    ).toBe(3_500);
  });

  test("returns the median of multiple samples", () => {
    const offset = estimateClockOffset([
      { sentAtMs: 0, receivedAtMs: 0, serverDateMs: 100 },
      { sentAtMs: 0, receivedAtMs: 0, serverDateMs: 900 },
      { sentAtMs: 0, receivedAtMs: 0, serverDateMs: 200 },
    ]);
    expect(offset).toBe(200);
  });
});

test.describe("fetchBeaconTokens (data layer)", () => {
  test("GETs /beacon/tokens?hours=24 with auth and parses the Date header", async () => {
    const dateHeader = "2030-01-01T00:00:00Z";
    let seenUrl = "";
    let seenAuth: string | null = null;
    handler = (url, init) => {
      seenUrl = url;
      seenAuth = new Headers(init?.headers).get("Authorization");
      return json(200, { windowSec: 900, tokens: [TOKEN_A] }, { Date: dateHeader });
    };

    const result = await fetchBeaconTokens();

    expect(seenUrl).toContain("/api/v1/beacon/tokens");
    expect(seenUrl).toContain("hours=24");
    expect(seenAuth).toBe("Bearer test-access-token");
    expect(result.windowSec).toBe(900);
    expect(result.tokens).toEqual([TOKEN_A]);
    expect(result.serverDateMs).toBe(Date.parse(dateHeader));
  });

  test("returns serverDateMs: null when the Date header is absent", async () => {
    handler = () => json(200, { windowSec: 900, tokens: [] });

    const result = await fetchBeaconTokens();

    expect(result.serverDateMs).toBeNull();
    expect(result.tokens).toEqual([]);
  });
});

test.describe("syncBeaconTokens", () => {
  test("skips fetch and native calls while the remaining window is >= 6h", async () => {
    const sink = recordingSink();
    let fetched = false;
    const now = Date.parse("2029-12-31T00:00:00Z");

    const result = await syncBeaconTokens({
      sink,
      existing: { windowSec: 900, tokens: [TOKEN_A] },
      nowMs: () => now,
      fetchTokens: async () => {
        fetched = true;
        throw new Error("should not fetch");
      },
    });

    expect(fetched).toBe(false);
    expect(result.refreshed).toBe(false);
    expect(sink.calls).toEqual([]);
  });

  test("refetches when the remaining window is < 6h and applies clock offset + tokens", async () => {
    const sink = recordingSink();
    const serverDateMs = Date.parse("2030-01-01T00:00:00Z");
    const now = Date.parse("2030-01-01T00:14:00Z");

    const result = await syncBeaconTokens({
      sink,
      existing: { windowSec: 900, tokens: [TOKEN_A] },
      nowMs: () => now,
      fetchTokens: async () => ({
        windowSec: 900,
        tokens: [TOKEN_A, TOKEN_B],
        serverDateMs,
        sentAtMs: 1_000,
        receivedAtMs: 2_000,
      }),
    });

    expect(result.refreshed).toBe(true);
    expect(sink.calls).toEqual([
      { kind: "clock", offsetMs: serverDateMs - 1_500 },
      { kind: "tokens", windowSec: 900, tokens: [TOKEN_A, TOKEN_B] },
    ]);
  });

  test("omits setClockOffset when the response has no Date header", async () => {
    const sink = recordingSink();

    await syncBeaconTokens({
      sink,
      fetchTokens: async () => ({
        windowSec: 900,
        tokens: [TOKEN_A],
        serverDateMs: null,
        sentAtMs: 0,
        receivedAtMs: 0,
      }),
    });

    expect(sink.calls).toEqual([{ kind: "tokens", windowSec: 900, tokens: [TOKEN_A] }]);
  });

  test("propagates the ApiError when the backend rejects (403)", async () => {
    handler = () => json(403, { error: { code: "forbidden", message: "denied" } });
    const sink = recordingSink();

    const error = await syncBeaconTokens({ sink }).catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(403);
    expect(sink.calls).toEqual([]);
  });
});

type SinkCall =
  | { kind: "clock"; offsetMs: number }
  | { kind: "tokens"; windowSec: number; tokens: BeaconToken[] };

function recordingSink(): BeaconSink & { calls: SinkCall[] } {
  const calls: SinkCall[] = [];
  return {
    calls,
    setBeaconTokens: async (opts) => {
      calls.push({ kind: "tokens", windowSec: opts.windowSec, tokens: opts.tokens });
    },
    setClockOffset: async (opts) => {
      calls.push({ kind: "clock", offsetMs: opts.offsetMs });
    },
  };
}
