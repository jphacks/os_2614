// DTO → ドメイン変換。純関数で例外を投げず、欠損は既定値で埋める。
// UI はこのファイルを経由した値だけを見る (DTO を UI に持ち込まない)。
//
// 入力の DTO は backend の JSON をそのまま写した形 (src/data/dto.ts)。
// 実データは任意項目が欠けたり null だったりしうるので、入力を `Maybe<DTO>`
// (= 深い Partial | null | undefined) として受け、全フィールドを既定値で埋める。
//
// 既定値は backend の schema.sql に合わせる:
//   encounterMode='delayed', encounterDelaySec=3600, friendBeaconShare=true,
//   partyAuto=true, locationPaused=false, warnDisplay='blur', notify={}

import type {
  AvatarTone,
  Encounter,
  EncounterMode,
  EncounterRelation,
  Friend,
  Group,
  GroupVisibility,
  LocationPrecision,
  Media,
  Memory,
  Party,
  PartyMember,
  PartyPhase,
  Post,
  PostTarget,
  Profile,
  Reply,
  Settings,
  Stats,
  User,
  WarnDisplay,
} from "@/domain/types";
import type {
  Encounter as EncounterDTO,
  Friend as FriendDTO,
  Group as GroupDTO,
  Instance as InstanceDTO,
  InstancePost as PostDTO,
  Media as MediaDTO,
  Memory as MemoryDTO,
  Party as PartyDTO,
  PartyMember as PartyMemberDTO,
  Profile as ProfileDTO,
  Settings as SettingsDTO,
  Stats as StatsDTO,
  User as UserDTO,
} from "./dto";

/** 深い Partial。DTO の任意項目 (= omitempty) を代入側でも省略できるようにする。 */
type DeepPartial<T> = T extends (infer U)[]
  ? DeepPartial<U>[]
  : T extends object
    ? { [K in keyof T]?: DeepPartial<T[K]> }
    : T;

/** 実データは欠損・null がありうるので、mapper はこの型で受ける。 */
type Maybe<T> = DeepPartial<T> | null | undefined;

const TONES: readonly AvatarTone[] = [
  "peach",
  "blue",
  "green",
  "lavender",
  "mint",
  "yellow",
  "periwinkle",
  "sand",
];

const PRECISIONS: readonly LocationPrecision[] = [
  "exact",
  "approx",
  "area",
  "direction",
  "off",
];
const ENCOUNTER_MODES: readonly EncounterMode[] = [
  "immediate",
  "delayed",
  "private",
];
const WARN_DISPLAYS: readonly WarnDisplay[] = ["show", "blur", "hide"];
const RELATIONS: readonly EncounterRelation[] = [
  "none",
  "requested",
  "incoming",
  "friend",
];
const PHASES: readonly PartyPhase[] = ["tentative", "confirmed"];
const VISIBILITIES: readonly GroupVisibility[] = ["open", "closed"];

/** 数値でなければ既定値にする。0 や負値は有効な値として通す。 */
function num(value: unknown, fallback = 0): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

/** DTO の語彙 (列挙) を検証し、未知の値は既定値へ寄せる。 */
function oneOf<T extends string>(
  value: unknown,
  allowed: readonly T[],
  fallback: T,
): T {
  return typeof value === "string" && (allowed as readonly string[]).includes(value)
    ? (value as T)
    : fallback;
}

/** notify は boolean の値だけを残した新しい Record にする。 */
function normalizeNotify(
  notify: Maybe<Record<string, boolean>>,
): Record<string, boolean> {
  const out: Record<string, boolean> = {};
  if (!notify) {
    return out;
  }
  for (const [key, value] of Object.entries(notify)) {
    if (typeof value === "boolean") {
      out[key] = value;
    }
  }
  return out;
}

/** ユーザー DID の文字コード和 mod 8 でアバター背景色を決める (決定的)。 */
export function avatarToneFor(didOrHandle: string): AvatarTone {
  let sum = 0;
  for (const char of didOrHandle) {
    sum += char.codePointAt(0) ?? 0;
  }
  const index = ((sum % TONES.length) + TONES.length) % TONES.length;
  return TONES[index];
}

/** UserSummary / AuthorSummary → User。name は displayName || handle。 */
export function toUser(dto: Maybe<UserDTO>): User {
  const did = dto?.did ?? "";
  const handle = dto?.handle ?? "";
  return {
    did,
    handle,
    name: dto?.displayName || handle,
    avatarUrl: dto?.avatarUrl,
    tone: avatarToneFor(did || handle),
  };
}

/** 誰も居ない author を表す空ユーザー (著者不明の投稿・返信に使う)。 */
function emptyUser(): User {
  return toUser(null);
}

/** MediaItem → Media。url / thumbUrl は署名付き URL。 */
export function toMedia(dto: Maybe<MediaDTO>): Media {
  return {
    id: dto?.id ?? "",
    alt: dto?.alt ?? "",
    url: dto?.url ?? "",
    thumbUrl: dto?.thumbUrl ?? "",
    width: num(dto?.width),
    height: num(dto?.height),
  };
}

/** Profile (GET /users/{did} / GET /me の平坦な形) → Profile。 */
export function toProfile(dto: Maybe<ProfileDTO>): Profile {
  return {
    ...toUser(dto),
    bio: dto?.description ?? "",
    status: dto?.status,
    bannerUrl: dto?.bannerUrl,
    // カウントは GET /me/stats (13 §3) が持つ。プロフィール単体では 0 から始める。
    counts: { encounters: 0, friends: 0, parties: 0 },
  };
}

/** FriendItem → Friend。precision の既定は "area"。 */
export function toFriend(dto: Maybe<FriendDTO>): Friend {
  return {
    ...toUser(dto),
    since: dto?.since ?? "",
    precision: oneOf(dto?.precision, PRECISIONS, "area"),
  };
}

/** EncounterItem → Encounter (06 §6)。relation の既定は "none"。 */
export function toEncounter(dto: Maybe<EncounterDTO>): Encounter {
  return {
    id: dto?.id ?? "",
    peer: toUser(dto?.peer),
    seenFrom: dto?.seenFrom ?? "",
    seenUntil: dto?.seenUntil ?? "",
    timesWithPeer: num(dto?.timesWithPeer),
    relation: oneOf(dto?.relation, RELATIONS, "none"),
  };
}

/** GroupDetail → Group。isMember を insideArea に写す。 */
export function toGroup(dto: Maybe<GroupDTO>): Group {
  return {
    id: dto?.id ?? "",
    name: dto?.name ?? "",
    visibility: oneOf(dto?.visibility, VISIBILITIES, "open"),
    memberCount: num(dto?.memberCount),
    // 半径 (geofence) がある = エリア型グループ。domain の表示ラベル。
    areaLabel: dto?.radiusM != null ? "GPSエリア" : "",
    insideArea: dto?.isMember ?? false,
  };
}

/** InstancePost → Post。moderation が付いた投稿は自動ピン扱い。 */
export function toPost(dto: Maybe<PostDTO>): Post {
  return {
    id: dto?.uri ?? "",
    author: dto?.author ? toUser(dto.author) : emptyUser(),
    body: dto?.text ?? "",
    createdAt: dto?.createdAt ?? "",
    attachments: (dto?.media ?? []).map(toMedia),
    replyCount: 0,
    likeCount: 0,
    likedByMe: false,
    // moderation / hidden は backend 側の判定。フラグ付きは自動ピンとして先頭に置く。
    pinned: Boolean(dto?.moderation),
  };
}

/** InstancePost → Reply (返信も同じ PostItem で返る)。 */
export function toReply(dto: Maybe<PostDTO>): Reply {
  return {
    id: dto?.uri ?? "",
    author: dto?.author ? toUser(dto.author) : emptyUser(),
    body: dto?.text ?? "",
    createdAt: dto?.createdAt ?? "",
    likeCount: 0,
    likedByMe: false,
  };
}

/**
 * InstanceItem → PostTarget (GET /instances, 13 §2)。type は backend が
 * "group" / "party" へ正規化した値をそのまま使う (10 §4.1 の投稿先)。
 */
export function toPostTarget(dto: Maybe<InstanceDTO>): PostTarget {
  return {
    spaceId: dto?.spaceId ?? "",
    type: dto?.type ?? "",
    name: dto?.name ?? "",
    memberCount: num(dto?.memberCount),
  };
}

/** PartyMemberItem → PartyMember。present / left は必須なので既定 false。 */
function toPartyMember(dto: Maybe<PartyMemberDTO>): PartyMember {
  return {
    did: dto?.did ?? "",
    handle: dto?.handle,
    displayName: dto?.displayName,
    avatarUrl: dto?.avatarUrl,
    present: dto?.present ?? false,
    left: dto?.left ?? false,
  };
}

/** PartyDetail → Party (08 §6)。members は在席 / 退出状態を持つ。 */
export function toParty(dto: Maybe<PartyDTO>): Party {
  return {
    id: dto?.id ?? "",
    name: dto?.name ?? "",
    phase: oneOf(dto?.phase, PHASES, "tentative"),
    locked: dto?.locked ?? false,
    tentativeAt: dto?.tentativeAt ?? "",
    confirmedAt: dto?.confirmedAt,
    members: (dto?.members ?? []).map(toPartyMember),
  };
}

/** MemoryItem → Memory (13 §2)。cover は任意。 */
export function toMemory(dto: Maybe<MemoryDTO>): Memory {
  return {
    spaceId: dto?.spaceId ?? "",
    type: dto?.type ?? "",
    name: dto?.name ?? "",
    from: dto?.from ?? "",
    until: dto?.until ?? "",
    memberCount: num(dto?.memberCount),
    mediaCount: num(dto?.mediaCount),
    cover: dto?.cover
      ? { thumbUrl: dto.cover.thumbUrl ?? "" }
      : undefined,
  };
}

/** Stats → Stats (13 §3)。欠けたカウントは 0。 */
export function toStats(dto: Maybe<StatsDTO>): Stats {
  return {
    encounters: {
      today: num(dto?.encounters?.today),
      week: num(dto?.encounters?.week),
      month: num(dto?.encounters?.month),
      total: num(dto?.encounters?.total),
      uniquePeople: num(dto?.encounters?.uniquePeople),
    },
    friends: num(dto?.friends),
    parties: { total: num(dto?.parties?.total) },
    groups: { member: num(dto?.groups?.member) },
    posts: { month: num(dto?.posts?.month) },
  };
}

/** meSettingsResponse → Settings。8 項目すべてに backend 既定値を入れる。 */
export function toSettings(dto: Maybe<SettingsDTO>): Settings {
  return {
    encounterMode: oneOf(dto?.encounterMode, ENCOUNTER_MODES, "delayed"),
    encounterDelaySec: num(dto?.encounterDelaySec, 3600),
    friendBeaconShare: dto?.friendBeaconShare ?? true,
    partyAuto: dto?.partyAuto ?? true,
    locationPaused: dto?.locationPaused ?? false,
    warnDisplay: oneOf(dto?.warnDisplay, WARN_DISPLAYS, "blur"),
    contentScanConsentVersion:
      typeof dto?.contentScanConsentVersion === "number"
        ? dto.contentScanConsentVersion
        : undefined,
    notify: normalizeNotify(dto?.notify),
  };
}

/**
 * PATCH /me/settings の body。backend の `settingsPatchRequest` は pointer なので、
 * 送らなかったキーは「変更なし」を意味する。与えられたキーだけを載せる。
 *
 * `locationPaused` は `PUT /me/location-sharing`、`contentScanConsentVersion` は
 * `PUT /me/consents` が所有するため、この PATCH には含めない。
 */
export type SettingsPatch = {
  encounterMode?: EncounterMode;
  encounterDelaySec?: number;
  friendBeaconShare?: boolean;
  partyAuto?: boolean;
  warnDisplay?: WarnDisplay;
  notify?: Record<string, boolean>;
};

/** 変更のあったキーだけを持つ PATCH body を作る。 */
export function fromSettings(patch: Partial<Settings>): SettingsPatch {
  const out: SettingsPatch = {};
  if (patch.encounterMode !== undefined) {
    out.encounterMode = patch.encounterMode;
  }
  if (patch.encounterDelaySec !== undefined) {
    out.encounterDelaySec = patch.encounterDelaySec;
  }
  if (patch.friendBeaconShare !== undefined) {
    out.friendBeaconShare = patch.friendBeaconShare;
  }
  if (patch.partyAuto !== undefined) {
    out.partyAuto = patch.partyAuto;
  }
  if (patch.warnDisplay !== undefined) {
    out.warnDisplay = patch.warnDisplay;
  }
  if (patch.notify !== undefined) {
    out.notify = patch.notify;
  }
  return out;
}
