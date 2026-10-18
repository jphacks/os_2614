import { expect, test } from "@playwright/test";

import { ApiError } from "../src/auth/errors";
import type { LocationFix } from "../src/device/proximity/definitions";
import {
  LOCATION_FOREGROUND_INTERVAL_MS,
  LOCATION_MAX_ACCURACY_M,
  LOCATION_MIN_UPLOAD_INTERVAL_MS,
  LOCATION_SMOOTHING_RESET_MS,
  LOCATION_SOURCE,
  isLocationUploadable,
  locationSkipReason,
  locationUploadDue,
  smoothLocationFix,
  toLocationUploadBody,
  uploadLocation,
} from "../src/device/proximity/location";

// 位置アップロード (PUT /me/location, 05 §4) を検証する。ブラウザは開かず、global fetch と
// localStorage だけをスタブして実モジュール (location.ts → data/api → auth/api) を通す。

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

function fix(overrides: Partial<LocationFix> = {}): LocationFix {
  return {
    lat: 35.6812,
    lon: 139.7671,
    accuracyM: 12.5,
    recordedAt: new Date().toISOString(),
    isMock: false,
    ...overrides,
  };
}

test.describe("locationSkipReason", () => {
  test("accepts a fresh, accurate, non-mock fix", () => {
    expect(locationSkipReason(fix(), Date.now())).toBeNull();
  });

  test("never uploads a mock location (backend rejects isMock=true)", () => {
    expect(locationSkipReason(fix({ isMock: true }), Date.now())).toBe("mock");
    expect(isLocationUploadable(fix({ isMock: true }), Date.now())).toBe(false);
  });

  test("skips fixes coarser than the 200m backend limit", () => {
    const coarse = fix({ accuracyM: LOCATION_MAX_ACCURACY_M + 1 });
    expect(locationSkipReason(coarse, Date.now())).toBe("low_accuracy");
  });

  test("skips fixes older than 120s and unparsable timestamps", () => {
    const now = Date.now();
    expect(locationSkipReason(fix({ recordedAt: new Date(now - 121_000).toISOString() }), now)).toBe(
      "stale",
    );
    expect(locationSkipReason(fix({ recordedAt: "not-a-date" }), now)).toBe("stale");
  });

  test("skips fixes more than 30s in the future", () => {
    const now = Date.now();
    expect(locationSkipReason(fix({ recordedAt: new Date(now + 31_000).toISOString() }), now)).toBe(
      "future",
    );
  });
});

test.describe("locationUploadDue (6/min throttle)", () => {
  test("keeps the minimum upload interval at 10 seconds or more", () => {
    expect(LOCATION_MIN_UPLOAD_INTERVAL_MS).toBeGreaterThanOrEqual(10_000);
    expect(LOCATION_FOREGROUND_INTERVAL_MS).toBeGreaterThanOrEqual(LOCATION_MIN_UPLOAD_INTERVAL_MS);
  });

  test("is due immediately when nothing has been sent yet", () => {
    expect(locationUploadDue({ lastSentAtMs: 0, nowMs: 0 })).toBe(true);
  });

  test("blocks a second send inside the 10s window and allows it at the boundary", () => {
    expect(locationUploadDue({ lastSentAtMs: 1_000, nowMs: 10_999 })).toBe(false);
    expect(locationUploadDue({ lastSentAtMs: 1_000, nowMs: 11_000 })).toBe(true);
  });

  test("honours a custom minimum interval", () => {
    expect(
      locationUploadDue({ lastSentAtMs: 1_000, nowMs: 6_999, minIntervalMs: 6_000 }),
    ).toBe(false);
    expect(
      locationUploadDue({ lastSentAtMs: 1_000, nowMs: 7_000, minIntervalMs: 6_000 }),
    ).toBe(true);
  });
});

test.describe("uploadLocation (data layer)", () => {
  test("PUTs the spec body with auth and resolves on 204", async () => {
    const recordedAt = new Date().toISOString();
    const requests: { url: string; method: string; auth: string | null; body: unknown }[] = [];
    handler = (url, init) => {
      requests.push({
        url,
        method: init?.method ?? "GET",
        auth: new Headers(init?.headers).get("Authorization"),
        body: init?.body ? JSON.parse(String(init.body)) : null,
      });
      return new Response(null, { status: 204 });
    };

    const theFix = fix({ lat: 35.0, lon: 135.0, accuracyM: 12.5, recordedAt, isMock: false });
    await uploadLocation(theFix);

    expect(requests).toHaveLength(1);
    expect(requests[0].url).toContain("/api/v1/me/location");
    expect(requests[0].method).toBe("PUT");
    expect(requests[0].auth).toBe("Bearer test-access-token");
    expect(requests[0].body).toEqual({
      lat: 35.0,
      lon: 135.0,
      accuracyM: 12.5,
      recordedAt,
      source: LOCATION_SOURCE,
      isMock: false,
    });
  });

  test("toLocationUploadBody always carries the isMock flag", () => {
    expect(toLocationUploadBody(fix({ isMock: false }))).toMatchObject({ isMock: false });
    expect(toLocationUploadBody(fix({ isMock: true }))).toMatchObject({ isMock: true });
  });

  test("surfaces a 400 from the backend as an ApiError", async () => {
    handler = () =>
      json(400, {
        error: { code: "invalid_request", message: "recordedAt is too old", details: {} },
      });

    const error = await uploadLocation(fix()).catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(400);
  });
});

test.describe("smoothLocationFix", () => {
  const t0 = Date.parse("2026-01-01T00:00:00.000Z");

  function at(msFromT0: number, overrides: Partial<LocationFix> = {}): LocationFix {
    return fix({ recordedAt: new Date(t0 + msFromT0).toISOString(), ...overrides });
  }

  test("returns next unchanged when there is no previous fix", () => {
    const next = at(0, { lat: 35.1, lon: 135.1 });
    expect(smoothLocationFix(null, next)).toBe(next);
  });

  test("returns next when the mock flag changes", () => {
    const prev = at(0, { isMock: false });
    const next = at(3_000, { isMock: true, lat: 35.0001, lon: 135.0001 });
    expect(smoothLocationFix(prev, next)).toBe(next);
  });

  test("resets to next when the fix is older than the reset window", () => {
    const prev = at(0, { lat: 35, lon: 135 });
    const next = at(LOCATION_SMOOTHING_RESET_MS + 1, { lat: 35.0001, lon: 135.0001 });
    expect(smoothLocationFix(prev, next)).toBe(next);
  });

  test("keeps prev when next is older than prev", () => {
    const prev = at(5_000, { lat: 35, lon: 135 });
    const next = at(0, { lat: 35.0001, lon: 135.0001 });
    expect(smoothLocationFix(prev, next)).toBe(prev);
  });

  test("blends two close fixes, landing between them with a smaller accuracy", () => {
    const prev = at(0, { lat: 35, lon: 135, accuracyM: 10 });
    const next = at(3_000, { lat: 35.0001, lon: 135.0001, accuracyM: 10 });
    const smoothed = smoothLocationFix(prev, next);

    expect(smoothed).not.toBe(next);
    expect(smoothed.lat).toBeGreaterThan(prev.lat);
    expect(smoothed.lat).toBeLessThan(next.lat);
    expect(smoothed.lon).toBeGreaterThan(prev.lon);
    expect(smoothed.lon).toBeLessThan(next.lon);
    expect(smoothed.accuracyM).toBeLessThan(prev.accuracyM);
    expect(smoothed.accuracyM).toBeLessThan(next.accuracyM);
    expect(smoothed.recordedAt).toBe(next.recordedAt);
  });

  test("barely moves for a coarse next fix", () => {
    const prev = at(0, { lat: 35, lon: 135, accuracyM: 10 });
    const next = at(3_000, { lat: 35.001, lon: 135, accuracyM: 150 });
    const smoothed = smoothLocationFix(prev, next);

    const moved = smoothed.lat - prev.lat;
    const gap = next.lat - prev.lat;
    expect(moved).toBeGreaterThan(0);
    expect(moved).toBeLessThan(gap * 0.05);
  });

  test("snaps to next when the jump is far beyond the error range", () => {
    const prev = at(0, { lat: 35, lon: 135, accuracyM: 10 });
    const next = at(3_000, { lat: 35.045, lon: 135, accuracyM: 10 });
    expect(smoothLocationFix(prev, next)).toBe(next);
  });
});
