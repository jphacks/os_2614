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
  Area,
  AvatarTone,
  Encounter,
  EncounterMode,
  EncounterRelation,
  FeedPost,
  Friend,
  FriendLocation,
  FriendRequest,
  Group,
  GroupGraph,
  GroupGraphEdgeKind,
  GroupInvite,
  GroupInviteInboxItem,
  GroupNearby,
  GroupVisibility,
  Invite,
  LocationPrecision,
  Media,
  Memory,
  ModerationDisplay,
  ModerationLabel,
  Notification,
  NotificationType,
  Party,
  PartyMember,
  PartyPhase,
  Pin,
  Post,
  PostFormat,
  PostModeration,
  PostTarget,
  Profile,
  Reply,
  Settings,
  Stats,
  User,
  WarnDisplay,
} from "@/domain/types";
import type {
  Area as AreaDTO,
  Encounter as EncounterDTO,
  FeedItem as FeedItemDTO,
  Friend as FriendDTO,
  FriendLocation as FriendLocationDTO,
  FriendRequest as FriendRequestDTO,
  Group as GroupDTO,
  GroupGraph as GroupGraphDTO,
  GroupInvite as GroupInviteDTO,
  GroupInviteInboxItem as GroupInviteInboxItemDTO,
  GroupNearby as GroupNearbyDTO,
  Instance as InstanceDTO,
  InstancePost as PostDTO,
  Invite as InviteDTO,
  Media as MediaDTO,
  Memory as MemoryDTO,
  ModerationInfo as ModerationInfoDTO,
  Notification as NotificationDTO,
  Party as PartyDTO,
  PartyMember as PartyMemberDTO,
  Pin as PinDTO,
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
const GROUP_EDGE_KINDS: readonly GroupGraphEdgeKind[] = [
  "contains",
  "overlaps",
  "shared_members",
];
const NOTIFICATION_TYPES: readonly NotificationType[] = [
  "friend.request",
  "friend.accepted",
  "party.confirmed",
  "group.nearby",
  "group.invited",
  "group.suggested",
  "encounter.daily",
  "encounter.weekly",
  "encounter.monthly",
  "moderation.hidden",
];
const PIN_REASONS: readonly Pin["reason"][] = ["auto", "manual"];
const POST_FORMATS: readonly PostFormat[] = ["plain", "markdown"];
const MODERATION_LABELS: readonly ModerationLabel[] = ["ok", "warn", "block"];
const MODERATION_DISPLAYS: readonly ModerationDisplay[] = ["show", "blur", "hide"];

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

/** FriendRequestItem → FriendRequest (07 §2.2)。status の既定は "pending"。 */
export function toFriendRequest(dto: Maybe<FriendRequestDTO>): FriendRequest {
  return {
    id: dto?.id ?? "",
    peer: toUser(dto?.peer),
    via: dto?.via ?? "",
    viaRef: dto?.viaRef,
    status: dto?.status ?? "pending",
    createdAt: dto?.createdAt ?? "",
  };
}

/** friendInviteResponse → Invite (07 §2.1)。code / url / expiresAt は必須。 */
export function toInvite(dto: Maybe<InviteDTO>): Invite {
  return {
    code: dto?.code ?? "",
    url: dto?.url ?? "",
    expiresAt: dto?.expiresAt ?? "",
  };
}

/**
 * friendLocationResponse → FriendLocation (07 §4.2)。precision ごとに項目が変わるため、
 * 欠けている項目は undefined のまま残し、既定 boolean だけ埋める。
 */
export function toFriendLocation(dto: Maybe<FriendLocationDTO>): FriendLocation {
  return {
    did: dto?.did ?? "",
    precision: oneOf(dto?.precision, PRECISIONS, "area"),
    stale: dto?.stale ?? false,
    updatedAt: dto?.updatedAt,
    lat: typeof dto?.lat === "number" ? dto.lat : undefined,
    lon: typeof dto?.lon === "number" ? dto.lon : undefined,
    accuracyM: typeof dto?.accuracyM === "number" ? dto.accuracyM : undefined,
    areaLabel: dto?.area?.label,
    outside: dto?.outside ?? false,
    direction: dto?.direction,
    distanceBucket: dto?.distanceBucket,
    viewerStale: dto?.viewerStale ?? false,
  };
}

/** FeedItem → FeedPost (17 §3.2)。著者不明は空ユーザーに寄せる。 */
export function toFeedPost(dto: Maybe<FeedItemDTO>): FeedPost {
  return {
    uri: dto?.uri ?? "",
    author: dto?.author ? toUser(dto.author) : emptyUser(),
    text: dto?.text ?? "",
    createdAt: dto?.createdAt ?? "",
    attachments: (dto?.media ?? []).map(toMedia),
  };
}

/** areaResponse → Area (07 §4.3)。欠損は空文字・0 で埋める。 */
export function toArea(dto: Maybe<AreaDTO>): Area {
  return {
    id: dto?.id ?? "",
    label: dto?.label ?? "",
    lat: num(dto?.lat),
    lon: num(dto?.lon),
    radiusM: num(dto?.radiusM),
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

/** NearbyResult → GroupNearby (09 §5)。inside は距離を返さず、near は帯だけ返す。 */
export function toGroupNearby(dto: Maybe<GroupNearbyDTO>): GroupNearby {
  return {
    inside: (dto?.inside ?? []).map((item) => ({
      id: item?.id ?? "",
      name: item?.name ?? "",
      memberCount: num(item?.memberCount),
      isMember: item?.isMember ?? false,
      persistent: item?.persistent ?? false,
      radiusM: num(item?.radiusM),
    })),
    near: (dto?.near ?? []).map((item) => ({
      id: item?.id ?? "",
      name: item?.name ?? "",
      memberCount: num(item?.memberCount),
      distanceBucket: item?.distanceBucket ?? "",
    })),
  };
}

/** InviteItem → GroupInvite (09 §6)。status の既定は "collecting"。 */
export function toGroupInvite(dto: Maybe<GroupInviteDTO>): GroupInvite {
  return {
    id: dto?.id ?? "",
    targetDid: dto?.targetDid ?? "",
    createdBy: dto?.createdBy ?? "",
    status: dto?.status ?? "collecting",
    approvedCount: num(dto?.approvedCount),
    requiredCount: num(dto?.requiredCount),
    createdAt: dto?.createdAt ?? "",
    expiresAt: dto?.expiresAt ?? "",
  };
}

/** InviteInboxItem → GroupInviteInboxItem (09 §6)。inviter は任意。 */
export function toGroupInviteInboxItem(
  dto: Maybe<GroupInviteInboxItemDTO>,
): GroupInviteInboxItem {
  return {
    id: dto?.id ?? "",
    spaceId: dto?.spaceId ?? "",
    groupName: dto?.groupName ?? "",
    inviter: dto?.inviter ? toUser(dto.inviter) : undefined,
    expiresAt: dto?.expiresAt ?? "",
  };
}

/** GraphResult → GroupGraph (09 §7)。中心座標は無い。weight は shared_members のときだけ。 */
export function toGroupGraph(dto: Maybe<GroupGraphDTO>): GroupGraph {
  return {
    nodes: (dto?.nodes ?? []).map((node) => ({
      id: node?.id ?? "",
      name: node?.name ?? "",
      radiusM: num(node?.radiusM),
      memberCount: num(node?.memberCount),
      isMember: node?.isMember ?? false,
      inside: node?.inside ?? false,
      origin: node?.origin,
    })),
    edges: (dto?.edges ?? []).map((edge) => ({
      from: edge?.from ?? "",
      to: edge?.to ?? "",
      kind: oneOf(edge?.kind, GROUP_EDGE_KINDS, "contains"),
      weight: typeof edge?.weight === "number" ? edge.weight : undefined,
    })),
  };
}

/** service.ModerationInfo → PostModeration (11 §4)。未知の値は ok/show に寄せる。 */
function toModeration(dto: Maybe<ModerationInfoDTO>): PostModeration | undefined {
  if (!dto) {
    return undefined;
  }
  return {
    label: oneOf(dto.label, MODERATION_LABELS, "ok"),
    display: oneOf(dto.display, MODERATION_DISPLAYS, "show"),
    categories: (dto.categories ?? []).filter(
      (category): category is string => typeof category === "string",
    ),
  };
}

/** InstancePost → Post。moderation の表示指示 (11 §4) はそのまま保持する。 */
export function toPost(dto: Maybe<PostDTO>): Post {
  const moderation = toModeration(dto?.moderation);
  return {
    id: dto?.uri ?? "",
    author: dto?.author ? toUser(dto.author) : emptyUser(),
    body: dto?.text ?? "",
    format: oneOf(dto?.format, POST_FORMATS, "plain"),
    createdAt: dto?.createdAt ?? "",
    attachments: (dto?.media ?? []).map(toMedia),
    replyCount: 0,
    likeCount: 0,
    likedByMe: false,
    // Pin は Space の Pin 一覧 (GET /instances/{spaceId}/pins, 10 §6) が正。
    // タイムラインの投稿は既定で非 Pin とし、呼び出し側が subjects で上書きする。
    pinned: false,
    ...(moderation ? { moderation } : {}),
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

/** Pin (10 §6) → Pin。reason の既定は "auto"。 */
export function toPin(dto: Maybe<PinDTO>): Pin {
  return {
    uri: dto?.uri ?? "",
    subject: dto?.subject ?? "",
    reason: oneOf(dto?.reason, PIN_REASONS, "auto"),
    pinnedBy: dto?.pinnedBy,
    createdAt: dto?.createdAt ?? "",
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

/** notificationResponse → Notification (12 §1)。未知の種類は "friend.request" に寄せる。 */
export function toNotification(dto: Maybe<NotificationDTO>): Notification {
  return {
    id: dto?.id ?? "",
    type: oneOf(dto?.type, NOTIFICATION_TYPES, "friend.request"),
    data: (dto?.data ?? {}) as Record<string, unknown>,
    createdAt: dto?.createdAt ?? "",
    readAt: dto?.readAt,
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
