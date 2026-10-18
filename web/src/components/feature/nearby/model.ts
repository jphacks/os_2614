// 「ちかく」に並べる泡を、フレンド・すれ違い・パーティー・グループのドメイン値から作る。
// 位置の解決 (どこにいるとみなすか) と、どのグループの範囲にいるか (縁の色) をここで決める。

import { avatarToneFor } from "@/data/mappers";
import type {
  AvatarTone,
  Encounter,
  Friend,
  FriendLocation,
  GroupNearby,
  Party,
  User,
} from "@/domain/types";
import {
  bearingDeg,
  bucketDistanceM,
  decodeGeohash,
  directionDeg,
  distanceM,
  insideCircle,
  type LatLon,
  type Placement,
} from "./layout";

/** グループの中心と半径。GET /groups/{id} がメンバーにだけ返す (09 §4)。 */
export type GroupArea = LatLon & { radiusM: number };

export type NearbyBubble =
  | { kind: "me"; id: string; size: number; user: User }
  | { kind: "plus"; id: string; size: number }
  | { kind: "party"; id: string; size: number; name: string; memberCount: number }
  | {
      kind: "person";
      id: string;
      size: number;
      user: User;
      source: "friend" | "encounter" | "party";
      /** 範囲内にいるグループの色。どこにもいなければ undefined。 */
      groupTone?: AvatarTone;
      groupName?: string;
      /** 位置の説明 (例: "約 300m"、"すれ違い 3回")。 */
      detail: string;
    }
  | {
      kind: "group";
      id: string;
      size: number;
      groupId: string;
      name: string;
      tone: AvatarTone;
      memberCount: number;
      isMember: boolean;
      /** 自分がこのグループのエリアの中にいる (参加・退出できる)。 */
      inside: boolean;
      detail: string;
    };

export type NearbyModel = {
  me: NearbyBubble & { kind: "me" };
  plus: NearbyBubble & { kind: "plus" };
  /** 自分に接して並べる泡 (パーティーとそのメンバー)。 */
  ring: NearbyBubble[];
  /** 距離と方角で並べる泡と、その位置。 */
  nodes: { bubble: NearbyBubble; placement: Placement }[];
};

export const SIZE_ME = 88;
export const SIZE_PLUS = 64;
export const SIZE_PARTY = 84;
export const SIZE_PERSON = 68;

/** BLE で見えている相手の説明。GPS より確かなので距離より優先する。 */
const BLE_NEARBY_DETAIL = "すぐそば";

/**
 * パーティーのメンバーを方角に置く最小の距離 (m)。これより近いと GPS の誤差で
 * 方角が当てにならないので、自分に接して並べる。
 */
const PARTY_DIRECTION_MIN_M = 10;

/** メンバー数に応じたグループの円の直径。 */
export function groupSize(memberCount: number): number {
  return Math.round(Math.min(136, 76 + 8 * Math.sqrt(Math.max(0, memberCount))));
}

/** id からグループの色を決める。アバターと同じパステルの並びを使う。 */
export function groupTone(id: string): AvatarTone {
  return avatarToneFor(`group:${id}`);
}

function formatDistance(m: number): string {
  if (m < 1_000) return `約 ${Math.max(10, Math.round(m / 10) * 10)}m`;
  return `約 ${(m / 1_000).toFixed(m < 10_000 ? 1 : 0)}km`;
}

type Resolved = { placement: Placement; point?: LatLon; detail: string };

const UNKNOWN: Resolved = { placement: { kind: "unknown" }, detail: "位置不明" };

function fromPoint(me: LatLon | null, point: LatLon, detail?: string): Resolved {
  if (!me) return { placement: { kind: "unknown" }, point, detail: detail ?? "位置不明" };
  const d = distanceM(me, point);
  return {
    placement: { kind: "known", distanceM: d, bearingDeg: bearingDeg(me, point) },
    point,
    detail: detail ?? formatDistance(d),
  };
}

/** フレンドの現在位置。見せてもらえる精度で解く。取れなければ最後のすれ違い場所。 */
function resolveFriend(
  me: LatLon | null,
  location: FriendLocation | undefined,
  encounter: Encounter | undefined,
): Resolved {
  if (location && !location.stale) {
    if (location.lat !== undefined && location.lon !== undefined) {
      return fromPoint(me, { lat: location.lat, lon: location.lon });
    }
    const distance = bucketDistanceM(location.distanceBucket);
    if (location.precision === "direction" && !location.viewerStale && distance !== undefined) {
      return {
        placement: { kind: "known", distanceM: distance, bearingDeg: directionDeg(location.direction) },
        detail: location.distanceBucket ?? "",
      };
    }
  }
  const viaEncounter = encounter ? resolveEncounter(me, encounter) : UNKNOWN;
  if (viaEncounter.placement.kind === "known") return viaEncounter;
  if (location?.areaLabel) return { ...UNKNOWN, detail: location.areaLabel };
  return UNKNOWN;
}

/** すれ違った相手は、すれ違った場所にいるとみなす (06 §7)。 */
function resolveEncounter(me: LatLon | null, encounter: Encounter): Resolved {
  const times = `すれ違い ${encounter.timesWithPeer}回`;
  const point = encounter.place ? decodeGeohash(encounter.place) : null;
  if (!point) return { ...UNKNOWN, detail: times };
  return fromPoint(me, point, times);
}

export function buildNearbyModel(input: {
  me: User;
  myLocation: LatLon | null;
  friends: readonly Friend[];
  friendLocations: ReadonlyMap<string, FriendLocation>;
  encounters: readonly Encounter[];
  party: Party | null;
  groups: GroupNearby | null;
  groupAreas: ReadonlyMap<string, GroupArea>;
  /**
   * 直近に BLE で見えたフレンドの DID。GPS の位置より優先して自分に接して並べる
   * (屋内や近距離では GPS の誤差の方が実際の距離より大きい)。
   */
  bleNearby?: ReadonlySet<string>;
}): NearbyModel {
  const { me, myLocation, groupAreas } = input;
  const bleNearby = input.bleNearby ?? new Set<string>();
  const ring: NearbyBubble[] = [];
  const shown = new Set<string>([me.did]);

  const party = input.party;
  let partyMembers: Party["members"] = [];
  if (party && party.phase !== "ended" && party.phase !== "dissolved") {
    const members = party.members.filter((m) => !m.left && m.did !== me.did);
    ring.push({
      kind: "party",
      id: `party:${party.id}`,
      size: SIZE_PARTY,
      name: party.name || "パーティー",
      memberCount: members.length + 1,
    });
    partyMembers = members;
  }

  // GET /encounters は新しい順なので、相手ごとに最初の 1 件が最新のすれ違い
  const latestEncounter = new Map<string, Encounter>();
  for (const encounter of input.encounters) {
    if (!latestEncounter.has(encounter.peer.did)) latestEncounter.set(encounter.peer.did, encounter);
  }

  const groupColor = (point: LatLon | undefined) => {
    if (!point || !input.groups) return undefined;
    let best: { tone: AvatarTone; name: string; radiusM: number } | undefined;
    for (const group of [...input.groups.inside, ...input.groups.near]) {
      const area = groupAreas.get(group.id);
      if (!area || !insideCircle(point, area, area.radiusM)) continue;
      // 重なっているときは小さい (より具体的な) グループの色にする
      if (!best || area.radiusM < best.radiusM) {
        best = { tone: groupTone(group.id), name: group.name, radiusM: area.radiusM };
      }
    }
    return best;
  };

  const nodes: NearbyModel["nodes"] = [];
  const pushPerson = (
    user: User,
    source: "friend" | "encounter" | "party",
    resolved: Resolved,
  ) => {
    shown.add(user.did);
    const color = groupColor(resolved.point);
    nodes.push({
      bubble: {
        kind: "person",
        id: `user:${user.did}`,
        size: SIZE_PERSON,
        user,
        source,
        groupTone: color?.tone,
        groupName: color?.name,
        detail: resolved.detail,
      },
      placement: resolved.placement,
    });
  };

  // パーティーのメンバーは自分に接して並べる。ただし位置が分かって少し離れている人は、
  // 方角が分かるよう他のフレンドと同じく方角へ置いて端末の向きで回す
  for (const member of partyMembers) {
    const name = member.displayName || member.handle || "メンバー";
    const user: User = {
      did: member.did,
      handle: member.handle ?? "",
      name,
      avatarUrl: member.avatarUrl,
      tone: avatarToneFor(member.did || name),
    };
    const status = member.present ? "パーティー" : "パーティー (離席中)";
    if (bleNearby.has(member.did)) {
      shown.add(member.did);
      ring.push({
        kind: "person",
        id: `user:${member.did}`,
        size: SIZE_PERSON,
        user,
        source: "party",
        detail: `${status}・${BLE_NEARBY_DETAIL}`,
      });
      continue;
    }
    const resolved = resolveFriend(
      myLocation,
      input.friendLocations.get(member.did),
      latestEncounter.get(member.did),
    );
    const placement = resolved.placement;
    if (
      placement.kind === "known" &&
      placement.bearingDeg !== undefined &&
      placement.distanceM >= PARTY_DIRECTION_MIN_M
    ) {
      pushPerson(user, "party", { ...resolved, detail: `${status}・${resolved.detail}` });
      continue;
    }
    shown.add(member.did);
    ring.push({
      kind: "person",
      id: `user:${member.did}`,
      size: SIZE_PERSON,
      user,
      source: "party",
      detail: status,
    });
  }

  for (const friend of input.friends) {
    if (shown.has(friend.did)) continue;
    if (bleNearby.has(friend.did)) {
      shown.add(friend.did);
      ring.push({
        kind: "person",
        id: `user:${friend.did}`,
        size: SIZE_PERSON,
        user: friend,
        source: "friend",
        detail: BLE_NEARBY_DETAIL,
      });
      continue;
    }
    pushPerson(
      friend,
      "friend",
      resolveFriend(myLocation, input.friendLocations.get(friend.did), latestEncounter.get(friend.did)),
    );
  }
  for (const [did, encounter] of latestEncounter) {
    if (shown.has(did)) continue;
    pushPerson(encounter.peer, "encounter", resolveEncounter(myLocation, encounter));
  }

  if (input.groups) {
    for (const group of input.groups.inside) {
      const area = groupAreas.get(group.id);
      // 中にいるので距離は 0 とみなす。中心が分かれば、その方角へ寄せる
      const placement: Placement =
        area && myLocation
          ? { kind: "known", distanceM: 0, bearingDeg: bearingDeg(myLocation, area) }
          : { kind: "known", distanceM: 0 };
      nodes.push({
        bubble: {
          kind: "group",
          id: `group:${group.id}`,
          size: groupSize(group.memberCount),
          groupId: group.id,
          name: group.name,
          tone: groupTone(group.id),
          memberCount: group.memberCount,
          isMember: group.isMember,
          inside: true,
          detail: "いまいるエリア",
        },
        placement,
      });
    }
    for (const group of input.groups.near) {
      const area = groupAreas.get(group.id);
      const distance = bucketDistanceM(group.distanceBucket);
      const placement: Placement =
        area && myLocation
          ? { kind: "known", distanceM: distanceM(myLocation, area), bearingDeg: bearingDeg(myLocation, area) }
          : distance !== undefined
            ? { kind: "known", distanceM: distance }
            : { kind: "unknown" };
      nodes.push({
        bubble: {
          kind: "group",
          id: `group:${group.id}`,
          size: groupSize(group.memberCount),
          groupId: group.id,
          name: group.name,
          tone: groupTone(group.id),
          memberCount: group.memberCount,
          isMember: false,
          inside: false,
          detail: group.distanceBucket || "近く",
        },
        placement,
      });
    }
  }

  return {
    me: { kind: "me", id: "me", size: SIZE_ME, user: me },
    plus: { kind: "plus", id: "plus", size: SIZE_PLUS },
    ring,
    nodes,
  };
}
