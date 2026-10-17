"use client";

// party の presence 送信 (backend 08 §2)。sighting の native リスナーは
// useNearbyTokens と共有し、ここはフレンド表・45 秒窓・送信間隔だけを持つ。

import { useCallback, useEffect, useRef } from "react";
import { ApiError } from "@/auth/errors";
import { getJson, postJson } from "@/data/api";
import { useRealtimeEvent } from "@/realtime/provider";
import type { SightingEvent } from "./definitions";

export const PARTY_PRESENCE_INTERVAL_MS = 15_000;
export const PARTY_EDGE_ALIVE_MS = 45_000;
export const PARTY_BEACON_REFRESH_MS = 3 * 60 * 60_000;
export const PARTY_BEACON_RETRY_MS = 60_000;

export type FriendBeaconResponse = {
  validUntil: string;
  friends: Array<{ did: string; tokens: Array<{ token: string }> }>;
};

export type PartyObservation = {
  did: string;
  lastSeenAt: string;
  rssi: number;
};

export type PartySighting = {
  did: string;
  lastSeenAt: string;
  rssi: number;
};

/** 表にない token は無視し、同じ DID は最も新しい観測だけにまとめる。 */
export function addPartySighting(
  table: ReadonlyMap<string, string>,
  sightings: Map<string, PartySighting>,
  event: SightingEvent,
): boolean {
  const did = table.get(event.token);
  if (did === undefined) {
    return false;
  }
  const previous = sightings.get(did);
  if (previous === undefined || Date.parse(event.seenAt) >= Date.parse(previous.lastSeenAt)) {
    sightings.set(did, { did, lastSeenAt: event.seenAt, rssi: event.rssi });
  }
  return true;
}

/** 補正済みの現在時刻から 45 秒以内の観測だけを返し、古いものを削除する。 */
export function partyObservations(
  sightings: Map<string, PartySighting>,
  nowMs: number,
  windowMs = PARTY_EDGE_ALIVE_MS,
): PartyObservation[] {
  const result: PartyObservation[] = [];
  for (const [did, sighting] of sightings) {
    const seenAtMs = Date.parse(sighting.lastSeenAt);
    if (!Number.isFinite(seenAtMs) || nowMs - seenAtMs > windowMs) {
      sightings.delete(did);
      continue;
    }
    result.push({ ...sighting });
  }
  return result;
}

/** 非空送信、または前回の非空状態を閉じる空送信が必要か。 */
export function shouldSendPartyPresence(
  observations: readonly PartyObservation[],
  sentNonEmpty: boolean,
): boolean {
  return observations.length > 0 || sentNonEmpty;
}

type ReporterOptions = {
  nowMs?: () => number;
  send?: (observations: PartyObservation[]) => Promise<unknown>;
  fetchBeacons?: () => Promise<FriendBeaconResponse>;
};

export class PartyPresenceReporter {
  private readonly nowMs: () => number;
  private readonly send: (observations: PartyObservation[]) => Promise<unknown>;
  private readonly fetchBeacons: () => Promise<FriendBeaconResponse>;
  private readonly table = new Map<string, string>();
  private readonly sightings = new Map<string, PartySighting>();
  private timer: ReturnType<typeof setInterval> | undefined;
  private beaconTimer: ReturnType<typeof setInterval> | undefined;
  private validUntilMs = 0;
  private lastBeaconAttemptMs = Number.NEGATIVE_INFINITY;
  private nextBeaconRefreshMs = Number.POSITIVE_INFINITY;
  private retryUntilMs = 0;
  private sentNonEmpty = false;

  constructor(options: ReporterOptions = {}) {
    this.nowMs = options.nowMs ?? (() => Date.now());
    this.send = options.send ?? ((observations) => postJson("/party/presence", { observations }));
    this.fetchBeacons =
      options.fetchBeacons ?? (() => getJson<FriendBeaconResponse>("/party/friend-beacons"));
  }


  get beaconTable(): ReadonlyMap<string, string> {
    return this.table;
  }

  get hasSentNonEmpty(): boolean {
    return this.sentNonEmpty;
  }

  setBeacons(response: FriendBeaconResponse): void {
    this.table.clear();
    for (const friend of response.friends) {
      for (const token of friend.tokens) {
        this.table.set(token.token, friend.did);
      }
    }
    this.validUntilMs = Date.parse(response.validUntil);
    this.nextBeaconRefreshMs = this.nowMs() + PARTY_BEACON_REFRESH_MS;
  }

  recordSighting(event: SightingEvent): boolean {
    return addPartySighting(this.table, this.sightings, event);
  }

  async tick(): Promise<boolean> {
    const now = this.nowMs();
    if (now < this.retryUntilMs) {
      return false;
    }
    const observations = partyObservations(this.sightings, now);
    if (!shouldSendPartyPresence(observations, this.sentNonEmpty)) {
      return false;
    }
    try {
      await this.send(observations);
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 429) {
        this.retryUntilMs = now + (cause.retryAfterMs ?? 60_000);
      }
      return false;
    }
    this.sentNonEmpty = observations.length > 0;
    return true;
  }

  async refreshBeacons(force = false): Promise<boolean> {
    const now = this.nowMs();
    if (!force && now - this.lastBeaconAttemptMs < PARTY_BEACON_RETRY_MS) {
      return false;
    }
    this.lastBeaconAttemptMs = now;
    try {
      this.setBeacons(await this.fetchBeacons());
      return true;
    } catch {
      return false;
    }
  }

  start(): void {
    void this.refreshBeacons(true);
    this.timer = setInterval(() => void this.tick(), PARTY_PRESENCE_INTERVAL_MS);
    this.beaconTimer = setInterval(() => {
      const now = this.nowMs();
      if (
        now >= this.nextBeaconRefreshMs ||
        now >= this.validUntilMs - 30 * 60_000
      ) {
        void this.refreshBeacons();
      }
    }, PARTY_BEACON_RETRY_MS);
  }

  stop(): void {
    if (this.timer !== undefined) clearInterval(this.timer);
    if (this.beaconTimer !== undefined) clearInterval(this.beaconTimer);
    this.timer = undefined;
    this.beaconTimer = undefined;
    this.table.clear();
    this.sightings.clear();
    this.sentNonEmpty = false;
  }
}

export function usePartyPresenceReporter(options: {
  enabled: boolean;
  clockOffsetMs: number;
}): (event: SightingEvent) => void {
  const reporterRef = useRef<PartyPresenceReporter | null>(null);
  const offsetRef = useRef(options.clockOffsetMs);
  useEffect(() => {
    reporterRef.current = new PartyPresenceReporter({
      nowMs: () => Date.now() + offsetRef.current,
    });
    return () => {
      reporterRef.current?.stop();
      reporterRef.current = null;
    };
  }, []);
  useEffect(() => {
    offsetRef.current = options.clockOffsetMs;
  }, [options.clockOffsetMs]);

  useRealtimeEvent("friend.accepted", () => {
    void reporterRef.current?.refreshBeacons();
  });
  useRealtimeEvent("friend.changed", () => {
    void reporterRef.current?.refreshBeacons();
  });

  useEffect(() => {
    const reporter = reporterRef.current;
    if (!reporter || !options.enabled) {
      return;
    }
    reporter.start();
    return () => reporter.stop();
  }, [options.enabled]);

  return useCallback((event: SightingEvent) => {
    reporterRef.current?.recordSighting(event);
  }, []);
}
