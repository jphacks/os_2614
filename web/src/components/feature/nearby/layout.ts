// 「ちかく」の泡の配置。自分を原点にしたワールド座標 (px、y は下向き) を返す。
//
// - 自分の右隣はグループ作成の「+」、パーティーとそのメンバーは自分に接するように置く。
//   この内側の輪は方角を持たないので、端末の向きでは回さない。
// - それ以外 (フレンド / すれ違い / グループ) は距離の近い順に、方角へ向けて外側へ置く。
//   重なりは北を上にした座標で解いておき、描画時に端末の向き (heading) だけ回す。
//   向きが変わっても並び順が崩れず、自分の周りを回るだけになる。
// - 位置がまったく分からないものは、いちばん外側にまとめる。

/** 地球上の点 (度)。 */
export type LatLon = { lat: number; lon: number };

/** 自分から見た相手の位置。bearingDeg が無いものは方角が分からない (決定的な角度に置く)。 */
export type Placement =
  | { kind: "known"; distanceM: number; bearingDeg?: number }
  | { kind: "unknown" };

export type NearbyNodeInput = {
  id: string;
  /** 直径 (px)。 */
  size: number;
  placement: Placement;
};

export type PlacedNode = {
  id: string;
  size: number;
  /** 北を上にした座標。heading で回す前の値。 */
  x: number;
  y: number;
};

export type InnerNodeInput = { id: string; size: number };

export type NearbyLayout = {
  /** 自分・+・パーティー。画面に固定 (回さない)。 */
  inner: PlacedNode[];
  /** 方角に合わせて回す泡。 */
  outer: PlacedNode[];
};

/** 泡どうしのすき間 (px)。 */
export const NODE_GAP = 12;
/** 内側の輪のすき間 (px)。下に出る名前が隣の泡に隠れないよう広めにとる。 */
const INNER_GAP = 18;
/** 内側の輪とそれ以外の間にあけるすき間 (px)。 */
const RING_GAP = 32;
/** 距離 (m) を半径 (px) に足すときの係数。log2(1 + d / DISTANCE_UNIT_M) 倍する。 */
const DISTANCE_PX = 30;
const DISTANCE_UNIT_M = 50;
/** 外側へ探すときの刻み (px)。 */
const RADIUS_STEP = 6;
/** 方角からずらしてよい最大の角度 (度)。これを超えるならさらに外へ出す。 */
const MAX_ANGLE_SHIFT = 50;
const ANGLE_STEP = 5;
/** 位置が分からないものを置き始める、配置済みの最外周からの距離 (px)。 */
const UNKNOWN_MARGIN = 48;

const EARTH_RADIUS_M = 6_371_000;
const toRad = (deg: number) => (deg * Math.PI) / 180;
const toDeg = (rad: number) => (rad * 180) / Math.PI;

/** 2 点間の大円距離 (m)。 */
export function distanceM(from: LatLon, to: LatLon): number {
  const dLat = toRad(to.lat - from.lat);
  const dLon = toRad(to.lon - from.lon);
  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(toRad(from.lat)) * Math.cos(toRad(to.lat)) * Math.sin(dLon / 2) ** 2;
  return 2 * EARTH_RADIUS_M * Math.asin(Math.min(1, Math.sqrt(a)));
}

/** from から to への方位角 (度、北 0・時計回り、0〜360)。 */
export function bearingDeg(from: LatLon, to: LatLon): number {
  const φ1 = toRad(from.lat);
  const φ2 = toRad(to.lat);
  const dλ = toRad(to.lon - from.lon);
  const y = Math.sin(dλ) * Math.cos(φ2);
  const x = Math.cos(φ1) * Math.sin(φ2) - Math.sin(φ1) * Math.cos(φ2) * Math.cos(dλ);
  return (toDeg(Math.atan2(y, x)) + 360) % 360;
}

const GEOHASH_BASE32 = "0123456789bcdefghjkmnpqrstuvwxyz";

/** geohash のセルの中心。不正な文字を含むときは null。 */
export function decodeGeohash(hash: string): LatLon | null {
  if (hash.length === 0) return null;
  let latMin = -90;
  let latMax = 90;
  let lonMin = -180;
  let lonMax = 180;
  let even = true;
  for (const char of hash.toLowerCase()) {
    const value = GEOHASH_BASE32.indexOf(char);
    if (value < 0) return null;
    for (let bit = 4; bit >= 0; bit -= 1) {
      const on = (value >> bit) & 1;
      if (even) {
        const mid = (lonMin + lonMax) / 2;
        if (on) lonMin = mid;
        else lonMax = mid;
      } else {
        const mid = (latMin + latMax) / 2;
        if (on) latMin = mid;
        else latMax = mid;
      }
      even = !even;
    }
  }
  return { lat: (latMin + latMax) / 2, lon: (lonMin + lonMax) / 2 };
}

const DIRECTIONS: Record<string, number> = {
  N: 0,
  NE: 45,
  E: 90,
  SE: 135,
  S: 180,
  SW: 225,
  W: 270,
  NW: 315,
};

/** direction 精度の 8 方位 (07 §4.2) を度にする。知らない値は undefined。 */
export function directionDeg(direction: string | undefined): number | undefined {
  return direction ? DIRECTIONS[direction.toUpperCase()] : undefined;
}

const BUCKETS: Record<string, number> = {
  "<100m": 60,
  "<500m": 300,
  "<2km": 1_200,
  "<10km": 6_000,
  far: 20_000,
};

/** 距離帯 (<100m / <500m / <2km / <10km / far) の代表値 (m)。知らない値は undefined。 */
export function bucketDistanceM(bucket: string | undefined): number | undefined {
  return bucket ? BUCKETS[bucket] : undefined;
}

/** id から決まる 0〜360 の角度。方角の分からない泡を毎回同じ場所に置くため。 */
export function stableAngle(id: string): number {
  let hash = 2166136261;
  for (let i = 0; i < id.length; i += 1) {
    hash ^= id.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return ((hash >>> 0) % 3600) / 10;
}

function overlaps(x: number, y: number, size: number, placed: readonly PlacedNode[]) {
  for (const other of placed) {
    const min = (size + other.size) / 2 + NODE_GAP;
    if ((x - other.x) ** 2 + (y - other.y) ** 2 < min * min) return true;
  }
  return false;
}

/** 北 0・時計回りの角度と半径 → 画面座標 (y は下向き)。 */
function polar(angleDeg: number, radius: number) {
  const rad = toRad(angleDeg);
  return { x: radius * Math.sin(rad), y: -radius * Math.cos(rad) };
}

/** 半径 rStart から外へ、角度 angle の左右を交互に探して最初に空いた場所に置く。 */
function placeOutward(
  id: string,
  size: number,
  angle: number,
  rStart: number,
  placed: readonly PlacedNode[],
): PlacedNode {
  for (let radius = rStart; ; radius += RADIUS_STEP) {
    for (let shift = 0; shift <= MAX_ANGLE_SHIFT; shift += ANGLE_STEP) {
      for (const sign of shift === 0 ? [1] : [1, -1]) {
        const { x, y } = polar(angle + sign * shift, radius);
        if (!overlaps(x, y, size, placed)) return { id, size, x, y };
      }
    }
  }
}

/**
 * 自分の周りに接する輪を作る。先頭は自分 (原点)、次に + を右隣、残り (パーティー →
 * メンバー) は左から時計回りに空いた所へ詰める。輪が埋まったら一段外へ出る。
 */
function layoutInner(
  me: InnerNodeInput,
  plus: InnerNodeInput | null,
  ring: readonly InnerNodeInput[],
): PlacedNode[] {
  const placed: PlacedNode[] = [{ ...me, x: 0, y: 0 }];
  if (plus) {
    placed.push({ ...plus, x: (me.size + plus.size) / 2 + NODE_GAP, y: 0 });
  }
  for (const node of ring) {
    // 左 (270°) から始め、自分に接する円周上を回って空きを探す
    let done = false;
    for (let radius = (me.size + node.size) / 2 + INNER_GAP; !done; radius += node.size / 2) {
      for (let step = 0; step < 360 && !done; step += ANGLE_STEP) {
        const angle = 270 + (step % 2 === 0 ? step / 2 : -(step + ANGLE_STEP) / 2);
        const { x, y } = polar(angle, radius);
        if (!overlaps(x, y, node.size + INNER_GAP - NODE_GAP, placed)) {
          placed.push({ ...node, x, y });
          done = true;
        }
      }
    }
  }
  return placed;
}

function extent(nodes: readonly PlacedNode[]) {
  let max = 0;
  for (const node of nodes) max = Math.max(max, Math.hypot(node.x, node.y) + node.size / 2);
  return max;
}

/**
 * 「ちかく」の配置を作る。nodes は距離の近い順に並べ直し、近いものから置く。
 * 結果の outer は北を上にした座標なので、描画時に rotate(heading) で回す。
 */
export function layoutNearby(input: {
  me: InnerNodeInput;
  plus: InnerNodeInput | null;
  ring: readonly InnerNodeInput[];
  nodes: readonly NearbyNodeInput[];
}): NearbyLayout {
  const inner = layoutInner(input.me, input.plus, input.ring);
  // 回したときに内側の輪とぶつからないよう、輪の外接円の外から置き始める
  const innerRadius = extent(inner) + RING_GAP;

  const known = input.nodes
    .filter((node) => node.placement.kind === "known")
    .sort((a, b) => distanceOf(a) - distanceOf(b) || a.id.localeCompare(b.id));
  const unknown = input.nodes
    .filter((node) => node.placement.kind === "unknown")
    .sort((a, b) => a.id.localeCompare(b.id));

  const outer: PlacedNode[] = [];
  // 近い順を保つため、前に置いたものより内側からは探し始めない
  let floor = 0;
  for (const node of known) {
    const placement = node.placement as Extract<Placement, { kind: "known" }>;
    const angle = placement.bearingDeg ?? stableAngle(node.id);
    const rStart = Math.max(
      innerRadius + node.size / 2,
      innerRadius + node.size / 2 + DISTANCE_PX * Math.log2(1 + placement.distanceM / DISTANCE_UNIT_M),
      floor,
    );
    const placed = placeOutward(node.id, node.size, angle, rStart, outer);
    outer.push(placed);
    floor = Math.max(floor, Math.hypot(placed.x, placed.y) - node.size);
  }

  const farRadius = Math.max(innerRadius, extent(outer)) + UNKNOWN_MARGIN;
  for (const node of unknown) {
    outer.push(
      placeOutward(node.id, node.size, stableAngle(node.id), farRadius + node.size / 2, outer),
    );
  }

  return { inner, outer };
}

function distanceOf(node: NearbyNodeInput) {
  return node.placement.kind === "known" ? node.placement.distanceM : Infinity;
}

/**
 * 北を上にした座標を、端末が headingDeg (北 0・時計回り) を向いているときの画面座標にする。
 * 端末が東を向くと、北にいた相手は左 (反時計回り) へ回る。
 */
export function rotateForHeading(node: PlacedNode, headingDeg: number): PlacedNode {
  if (headingDeg === 0) return node;
  const rad = toRad(-headingDeg);
  const cos = Math.cos(rad);
  const sin = Math.sin(rad);
  return {
    ...node,
    x: node.x * cos - node.y * sin,
    y: node.x * sin + node.y * cos,
  };
}

/** 点が円 (中心 + 半径 m) の中にあるか。 */
export function insideCircle(point: LatLon, center: LatLon, radiusM: number): boolean {
  return distanceM(point, center) <= radiusM;
}
