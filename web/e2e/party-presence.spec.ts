import { expect, test } from "@playwright/test";
import { ApiError } from "../src/auth/errors";
import type { SightingEvent } from "../src/device/proximity/definitions";
import {
  addPartySighting,
  partyObservations,
  PartyPresenceReporter,
  shouldSendPartyPresence,
} from "../src/device/proximity/party";

function iso(ms: number): string {
  return new Date(ms).toISOString();
}

const event = (token: string, seenAt: string, rssi = -60): SightingEvent => ({
  token,
  seenAt,
  rssi,
});

test.describe("party presence", () => {
  test("keeps only observations inside the 45 second window", () => {
    const now = Date.parse("2030-01-01T00:01:00Z");
    const table = new Map([["token-a", "did:a"]]);
    const sightings = new Map();
    addPartySighting(table, sightings, event("token-a", iso(now - 44_000)));
    expect(partyObservations(sightings, now)).toHaveLength(1);
    sightings.set("did:a", { did: "did:a", lastSeenAt: iso(now - 46_000), rssi: -60 });
    expect(partyObservations(sightings, now)).toHaveLength(0);
  });

  test("ignores unknown tokens and sends an empty observation only once", async () => {
    let now = Date.parse("2030-01-01T00:00:00Z");
    const sent: unknown[] = [];
    const reporter = new PartyPresenceReporter({
      nowMs: () => now,
      send: async (observations) => void sent.push(observations),
    });
    reporter.setBeacons({ validUntil: iso(now + 3_600_000), friends: [{ did: "did:a", tokens: [{ token: "known" }] }] });
    expect(reporter.recordSighting(event("unknown", iso(now)))).toBe(false);
    await reporter.tick();
    expect(sent).toEqual([]);
    reporter.recordSighting(event("known", iso(now)));
    await reporter.tick();
    expect(sent).toHaveLength(1);
    now += 46_000;
    await reporter.tick();
    expect(sent).toHaveLength(2);
    await reporter.tick();
    expect(sent).toHaveLength(2);
    expect(shouldSendPartyPresence([], true)).toBe(true);
  });

  test("does not send during Retry-After", async () => {
    let now = Date.parse("2030-01-01T00:00:00Z");
    let calls = 0;
    const reporter = new PartyPresenceReporter({
      nowMs: () => now,
      send: async () => {
        calls += 1;
        if (calls === 1) throw new ApiError(429, "rate_limited", "wait", undefined, 10_000);
      },
    });
    reporter.setBeacons({ validUntil: iso(now + 3_600_000), friends: [{ did: "did:a", tokens: [{ token: "known" }] }] });
    reporter.recordSighting(event("known", iso(now)));
    await reporter.tick();
    now += 9_999;
    await reporter.tick();
    expect(calls).toBe(1);
    now += 1;
    await reporter.tick();
    expect(calls).toBe(2);
  });

  test("refreshes beacons at startup, before expiry, and after the retry interval", async () => {
    let now = Date.parse("2030-01-01T00:00:00Z");
    let fetches = 0;
    const reporter = new PartyPresenceReporter({
      nowMs: () => now,
      fetchBeacons: async () => {
        fetches += 1;
        return { validUntil: iso(now + 31 * 60_000), friends: [] };
      },
    });
    await reporter.refreshBeacons(true);
    expect(fetches).toBe(1);
    await reporter.refreshBeacons();
    expect(fetches).toBe(1);
    now += 60_000;
    await reporter.refreshBeacons();
    expect(fetches).toBe(2);
  });
});
