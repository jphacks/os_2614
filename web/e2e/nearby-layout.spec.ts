import { expect, test } from "@playwright/test";

import {
  bearingDeg,
  bucketDistanceM,
  decodeGeohash,
  directionDeg,
  distanceM,
  layoutNearby,
  NODE_GAP,
  rotateForHeading,
  stableAngle,
  type NearbyLayout,
  type PlacedNode,
} from "../src/components/feature/nearby/layout";
import { buildNearbyModel } from "../src/components/feature/nearby/model";
import type {
  Encounter,
  Friend,
  FriendLocation,
  Party,
  User,
} from "../src/domain/types";

// 純関数のテスト。ブラウザもサーバーも使わないので決定的に速く回る。

const ME: User = { did: "did:me", handle: "me", name: "じぶん", tone: "blue" };

function user(did: string, name: string): User {
  return { did, handle: did.replace("did:", ""), name, tone: "green" };
}

function distanceToOrigin(node: PlacedNode): number {
  return Math.hypot(node.x, node.y);
}

function distanceBetween(a: PlacedNode, b: PlacedNode): number {
  return Math.hypot(a.x - b.x, a.y - b.y);
}

function findNode(nodes: readonly PlacedNode[], id: string): PlacedNode {
  const node = nodes.find((candidate) => candidate.id === id);
  expect(node, `node ${id} not found`).toBeDefined();
  return node as PlacedNode;
}

test.describe("distanceM", () => {
  test("returns zero for the same point", () => {
    expect(distanceM({ lat: 35, lon: 135 }, { lat: 35, lon: 135 })).toBe(0);
  });

  test("grows with the gap between the points", () => {
    const from = { lat: 35, lon: 135 };
    const near = distanceM(from, { lat: 35.001, lon: 135 });
    const far = distanceM(from, { lat: 35.01, lon: 135 });
    expect(near).toBeGreaterThan(100);
    expect(near).toBeLessThan(120);
    expect(far).toBeGreaterThan(near);
  });
});

test.describe("bearingDeg", () => {
  test("returns the four cardinal directions", () => {
    const from = { lat: 0, lon: 0 };
    expect(bearingDeg(from, { lat: 1, lon: 0 })).toBeCloseTo(0, 5);
    expect(bearingDeg(from, { lat: 0, lon: 1 })).toBeCloseTo(90, 5);
    expect(bearingDeg(from, { lat: -1, lon: 0 })).toBeCloseTo(180, 5);
    expect(bearingDeg(from, { lat: 0, lon: -1 })).toBeCloseTo(270, 5);
  });
});

test.describe("decodeGeohash", () => {
  test("decodes a 6-char geohash near lat 35.008 / lon 135.006", () => {
    const point = decodeGeohash("xn0p0j");
    expect(point).not.toBeNull();
    expect((point as { lat: number }).lat).toBeCloseTo(35.008, 1);
    expect((point as { lon: number }).lon).toBeCloseTo(135.006, 1);
  });

  test("returns null for an invalid character", () => {
    expect(decodeGeohash("xn0p0a")).toBeNull();
    expect(decodeGeohash("")).toBeNull();
  });
});

test.describe("directionDeg", () => {
  test("maps the 8 directions to degrees, case-insensitively", () => {
    expect(directionDeg("N")).toBe(0);
    expect(directionDeg("ne")).toBe(45);
    expect(directionDeg("E")).toBe(90);
    expect(directionDeg("SW")).toBe(225);
  });

  test("returns undefined for an unknown or missing value", () => {
    expect(directionDeg("X")).toBeUndefined();
    expect(directionDeg(undefined)).toBeUndefined();
  });
});

test.describe("bucketDistanceM", () => {
  test("maps the buckets to their representative distances", () => {
    expect(bucketDistanceM("<100m")).toBe(60);
    expect(bucketDistanceM("<500m")).toBe(300);
    expect(bucketDistanceM("<2km")).toBe(1_200);
    expect(bucketDistanceM("<10km")).toBe(6_000);
    expect(bucketDistanceM("far")).toBe(20_000);
  });

  test("returns undefined for an unknown bucket", () => {
    expect(bucketDistanceM("nope")).toBeUndefined();
    expect(bucketDistanceM(undefined)).toBeUndefined();
  });
});

test.describe("stableAngle", () => {
  test("is deterministic and within 0..360", () => {
    for (const id of ["user:a", "user:b", "group:g1", "party:p1"]) {
      const first = stableAngle(id);
      expect(stableAngle(id)).toBe(first);
      expect(first).toBeGreaterThanOrEqual(0);
      expect(first).toBeLessThan(360);
    }
  });
});

test.describe("rotateForHeading", () => {
  test("returns the node unchanged when facing north", () => {
    const node: PlacedNode = { id: "a", size: 56, x: 10, y: -20 };
    expect(rotateForHeading(node, 0)).toBe(node);
  });

  test("moves a northern bubble to the left when facing east", () => {
    const north: PlacedNode = { id: "a", size: 56, x: 0, y: -100 };
    const rotated = rotateForHeading(north, 90);
    expect(rotated.x).toBeCloseTo(-100, 5);
    expect(rotated.y).toBeCloseTo(0, 5);
  });
});

test.describe("layoutNearby", () => {
  const me = { id: "me", size: 76 };
  const plus = { id: "plus", size: 48 };

  test("places me at the origin and + to the right on the same row", () => {
    const layout = layoutNearby({ me, plus, ring: [], nodes: [] });
    const mine = findNode(layout.inner, "me");
    expect(mine.x).toBe(0);
    expect(mine.y).toBe(0);

    const plusNode = findNode(layout.inner, "plus");
    expect(plusNode.y).toBe(0);
    expect(plusNode.x).toBeCloseTo((76 + 48) / 2 + NODE_GAP, 5);
  });

  test("puts ring bubbles touching me without overlapping", () => {
    const layout = layoutNearby({
      me,
      plus,
      ring: [{ id: "p1", size: 56 }],
      nodes: [],
    });
    const mine = findNode(layout.inner, "me");
    const plusNode = findNode(layout.inner, "plus");
    const ringNode = findNode(layout.inner, "p1");

    // 自分に接する (中心間距離 >= 半径の和)。重なりはしない。
    expect(distanceBetween(ringNode, mine)).toBeGreaterThanOrEqual((76 + 56) / 2 - 1);
    expect(distanceBetween(ringNode, plusNode)).toBeGreaterThanOrEqual((56 + 48) / 2 - 1);
  });

  test("keeps outer bubbles outside the inner ring and non-overlapping", () => {
    const layout = layoutNearby({
      me,
      plus,
      ring: [],
      nodes: [
        { id: "a", size: 56, placement: { kind: "known", distanceM: 100, bearingDeg: 0 } },
      ],
    });
    const mine = findNode(layout.inner, "me");
    const plusNode = findNode(layout.inner, "plus");
    const outer = findNode(layout.outer, "a");

    // 内側の輪 (自分 + 半径) より外側にいる。
    expect(distanceToOrigin(outer)).toBeGreaterThan((76 + 56) / 2);
    expect(distanceBetween(outer, mine)).toBeGreaterThanOrEqual((56 + 76) / 2);
    expect(distanceBetween(outer, plusNode)).toBeGreaterThanOrEqual((56 + 48) / 2);
    // 方角 0 (北) なので上側。
    expect(outer.y).toBeLessThan(0);
  });

  test("orders known bubbles from near to far", () => {
    const layout = layoutNearby({
      me,
      plus,
      ring: [],
      nodes: [
        { id: "c", size: 56, placement: { kind: "known", distanceM: 20_000, bearingDeg: 180 } },
        { id: "a", size: 56, placement: { kind: "known", distanceM: 100, bearingDeg: 0 } },
        { id: "b", size: 56, placement: { kind: "known", distanceM: 5_000, bearingDeg: 90 } },
      ],
    });
    expect(layout.outer.map((node) => node.id)).toEqual(["a", "b", "c"]);
    const [a, b, c] = layout.outer;
    expect(distanceToOrigin(a)).toBeLessThan(distanceToOrigin(b));
    expect(distanceToOrigin(b)).toBeLessThan(distanceToOrigin(c));
  });

  test("places unknown bubbles beyond every known bubble", () => {
    const layout = layoutNearby({
      me,
      plus,
      ring: [],
      nodes: [
        { id: "a", size: 56, placement: { kind: "known", distanceM: 100, bearingDeg: 0 } },
        { id: "u", size: 56, placement: { kind: "unknown" } },
      ],
    });
    const a = findNode(layout.outer, "a");
    const u = findNode(layout.outer, "u");
    expect(distanceToOrigin(u)).toBeGreaterThan(distanceToOrigin(a));
  });

  test("is deterministic for the same input", () => {
    const input = {
      me,
      plus,
      ring: [{ id: "p1", size: 56 }],
      nodes: [
        { id: "a", size: 56, placement: { kind: "known" as const, distanceM: 300 } },
        { id: "u", size: 56, placement: { kind: "unknown" as const } },
      ],
    };
    const first = layoutNearby(input);
    const second = layoutNearby(input);
    expect(second).toEqual(first);
  });

  test("never overlaps any pair of placed bubbles", () => {
    const layout: NearbyLayout = layoutNearby({
      me,
      plus,
      ring: [
        { id: "p1", size: 56 },
        { id: "p2", size: 56 },
      ],
      nodes: [
        { id: "a", size: 56, placement: { kind: "known", distanceM: 50, bearingDeg: 0 } },
        { id: "b", size: 56, placement: { kind: "known", distanceM: 80, bearingDeg: 10 } },
        { id: "u", size: 56, placement: { kind: "unknown" } },
      ],
    });
    const all = [...layout.inner, ...layout.outer];
    for (let i = 0; i < all.length; i += 1) {
      for (let j = i + 1; j < all.length; j += 1) {
        const min = (all[i].size + all[j].size) / 2;
        expect(
          distanceBetween(all[i], all[j]),
          `${all[i].id} overlaps ${all[j].id}`,
        ).toBeGreaterThanOrEqual(min - 1);
      }
    }
  });
});

test.describe("buildNearbyModel", () => {
  const myLocation = { lat: 35, lon: 135 };

  function location(overrides: Partial<FriendLocation>): FriendLocation {
    return {
      did: "did:x",
      precision: "exact",
      stale: false,
      outside: false,
      viewerStale: false,
      ...overrides,
    };
  }

  function friend(did: string, name: string, precision: Friend["precision"]): Friend {
    return { ...user(did, name), since: "2026-01-01T00:00:00Z", precision };
  }

  const friends: Friend[] = [
    friend("did:exact", "エクサクト", "exact"),
    friend("did:stale", "ステイル", "exact"),
    friend("did:off", "オフ", "off"),
    friend("did:enc", "エンカウント", "exact"),
  ];

  const friendLocations = new Map<string, FriendLocation>([
    ["did:exact", location({ did: "did:exact", lat: 35.001, lon: 135.001 })],
    ["did:stale", location({ did: "did:stale", stale: true })],
    ["did:off", location({ did: "did:off", precision: "off" })],
    ["did:enc", location({ did: "did:enc", stale: true })],
  ]);

  const encounters: Encounter[] = [
    {
      id: "e1",
      peer: user("did:enc", "エンカウント"),
      seenFrom: "2026-01-01T00:00:00Z",
      seenUntil: "2026-01-01T01:00:00Z",
      timesWithPeer: 3,
      relation: "none",
      place: "xn0p0j",
    },
  ];

  const party: Party = {
    id: "p1",
    name: "昼ごはん",
    phase: "confirmed",
    locked: false,
    tentativeAt: "2026-01-01T00:00:00Z",
    members: [
      { did: "did:me", present: true, left: false },
      { did: "did:pm", handle: "pm", displayName: "メンバー", present: true, left: false },
    ],
  };

  function build() {
    return buildNearbyModel({
      me: ME,
      myLocation,
      friends,
      friendLocations,
      encounters,
      party,
      groups: {
        inside: [
          {
            id: "g1",
            name: "情報工学科 3年",
            memberCount: 12,
            isMember: false,
            persistent: true,
            radiusM: 120,
          },
        ],
        near: [],
      },
      groupAreas: new Map([["g1", { lat: 35.001, lon: 135.001, radiusM: 120 }]]),
    });
  }

  test("puts party members in the ring and never duplicates them in nodes", () => {
    const model = build();
    const partyBubble = model.ring.find((bubble) => bubble.kind === "party");
    expect(partyBubble).toBeDefined();
    expect(partyBubble?.kind === "party" && partyBubble.name).toBe("昼ごはん");
    expect(partyBubble?.kind === "party" && partyBubble.memberCount).toBe(2);

    const memberBubble = model.ring.find((bubble) => bubble.id === "user:did:pm");
    expect(memberBubble?.kind).toBe("person");

    const nodeIds = model.nodes.map((node) => node.bubble.id);
    expect(nodeIds).not.toContain("user:did:pm");
  });

  test("treats an exact friend location as known", () => {
    const model = build();
    const node = model.nodes.find((entry) => entry.bubble.id === "user:did:exact");
    expect(node?.placement.kind).toBe("known");
  });

  test("treats stale and off friends as unknown", () => {
    const model = build();
    expect(
      model.nodes.find((entry) => entry.bubble.id === "user:did:stale")?.placement.kind,
    ).toBe("unknown");
    expect(
      model.nodes.find((entry) => entry.bubble.id === "user:did:off")?.placement.kind,
    ).toBe("unknown");
  });

  test("resolves a friend through the encounter place as known", () => {
    const model = build();
    const node = model.nodes.find((entry) => entry.bubble.id === "user:did:enc");
    expect(node?.placement.kind).toBe("known");
    expect(node?.bubble.kind === "person" && node.bubble.detail).toContain("すれ違い");
  });

  test("colors a friend inside a group area with the group tone", () => {
    const model = build();
    const node = model.nodes.find((entry) => entry.bubble.id === "user:did:exact");
    expect(node?.bubble.kind === "person" && node.bubble.groupTone).toBeDefined();
    expect(node?.bubble.kind === "person" && node.bubble.groupName).toBe("情報工学科 3年");
  });

  test("includes the inside group as a node", () => {
    const model = build();
    const node = model.nodes.find((entry) => entry.bubble.id === "group:g1");
    expect(node?.bubble.kind).toBe("group");
    expect(node?.bubble.kind === "group" && node.bubble.inside).toBe(true);
  });
});
