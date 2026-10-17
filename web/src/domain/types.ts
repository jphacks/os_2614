// UI が依存するドメイン型。backend の DTO (src/data/dto.ts) とは src/data/mappers.ts で変換する。
// 形は backend の JSON (camelCase) に合わせる。表示用の派生名 (User.name) だけ画面向けに足す。

export type DID = string;
export type ID = string;
/** RFC 3339 の UTC 時刻 */
export type Timestamp = string;

/** アバター背景のパステル色。ユーザー DID から決定的に選ぶ (avatarToneFor) */
export type AvatarTone =
  | "peach"
  | "blue"
  | "green"
  | "lavender"
  | "mint"
  | "yellow"
  | "periwinkle"
  | "sand";

/** ユーザーの要約 (service.UserSummary / AuthorSummary)。 */
export type User = {
  did: DID;
  handle: string;
  /** 表示名。displayName || handle から導出する */
  name: string;
  /** 画像が無いときは name の先頭 1 文字を表示する */
  avatarUrl?: string;
  tone: AvatarTone;
};

/** GET /users/{did} / GET /me のプロフィール (13 §1)。 */
export type Profile = User & {
  bio: string;
  /** 例: "図書館でもくもく中" */
  status?: string;
  bannerUrl?: string;
  counts: { encounters: number; friends: number; parties: number };
};

/** 位置の見せ方 (07 §4.1)。フレンドごとに設定する。 */
export type LocationPrecision = "exact" | "approx" | "area" | "direction" | "off";

/** フレンド (service.FriendItem、07 §2.3)。 */
export type Friend = User & {
  since: Timestamp;
  precision: LocationPrecision;
};

/** 相手との関係 (06 §6)。 */
export type EncounterRelation = "none" | "requested" | "incoming" | "friend";

/** すれ違いの 1 件 (service.EncounterItem、06 §6)。 */
export type Encounter = {
  id: ID;
  peer: User;
  seenFrom: Timestamp;
  seenUntil: Timestamp;
  timesWithPeer: number;
  relation: EncounterRelation;
};

export type GroupVisibility = "open" | "closed";

export type Group = {
  id: ID;
  name: string;
  iconUrl?: string;
  visibility: GroupVisibility;
  memberCount: number;
  /** 例: "GPSエリア" */
  areaLabel: string;
  /** エリア内にいるときだけ投稿できる */
  insideArea: boolean;
};

/** メディア (service.MediaItem、10 §4.4)。url / thumbUrl は署名付き URL。 */
export type Media = {
  id: ID;
  alt: string;
  url: string;
  thumbUrl: string;
  width: number;
  height: number;
};

export type Post = {
  id: ID;
  author: User;
  body: string;
  createdAt: Timestamp;
  attachments: Media[];
  replyCount: number;
  likeCount: number;
  likedByMe: boolean;
  /** 自動ピン (moderation 側で判定) */
  pinned: boolean;
};

export type Reply = {
  id: ID;
  author: User;
  body: string;
  createdAt: Timestamp;
  likeCount: number;
  likedByMe: boolean;
};

/** 公開投稿 (service.FeedItem の表示用、17 §3.2)。 */
export type FeedPost = {
  uri: string;
  author: User;
  text: string;
  createdAt: Timestamp;
  attachments: Media[];
};

/** party の段階 (08 §4.2)。 */
export type PartyPhase = "tentative" | "confirmed";

/** party のメンバー (service.PartyMemberItem、08 §6)。 */
export type PartyMember = {
  did: DID;
  handle?: string;
  displayName?: string;
  avatarUrl?: string;
  present: boolean;
  left: boolean;
};

/** GET /party/current の応答 (service.PartyDetail、08 §6)。 */
export type Party = {
  id: ID;
  name: string;
  phase: PartyPhase;
  locked: boolean;
  tentativeAt: Timestamp;
  confirmedAt?: Timestamp;
  members: PartyMember[];
};

/** memory の表紙 (service.MemoryCover、13 §2)。 */
export type MemoryCover = { thumbUrl: string };

/** GET /memories の 1 件 (service.MemoryItem、13 §2)。 */
export type Memory = {
  spaceId: string;
  type: string;
  name: string;
  from: Timestamp;
  until: Timestamp;
  memberCount: number;
  mediaCount: number;
  cover?: MemoryCover;
};

/** GET /me/stats の集計 (13 §3)。 */
export type Stats = {
  encounters: {
    today: number;
    week: number;
    month: number;
    total: number;
    uniquePeople: number;
  };
  friends: number;
  parties: { total: number };
  groups: { member: number };
  posts: { month: number };
};

/** すれ違いの公開モード (06 §5.1)。 */
export type EncounterMode = "immediate" | "delayed" | "private";

/** moderation 警告の表示方法 (11 §1)。 */
export type WarnDisplay = "show" | "blur" | "hide";

/** GET /me の設定 (controller.meSettingsResponse、06 §5.1 / 07 §4 / 12 §3)。 */
export type Settings = {
  encounterMode: EncounterMode;
  encounterDelaySec: number;
  friendBeaconShare: boolean;
  partyAuto: boolean;
  locationPaused: boolean;
  warnDisplay: WarnDisplay;
  contentScanConsentVersion?: number;
  notify: Record<string, boolean>;
};

/** 通知の種類 (12 §3)。 */
export type NotificationType =
  | "friend.request"
  | "friend.accepted"
  | "party.confirmed"
  | "group.nearby"
  | "group.invited"
  | "group.suggested"
  | "encounter.daily"
  | "encounter.weekly"
  | "encounter.monthly"
  | "moderation.hidden";

/** GET /notifications の 1 件 (controller.notificationResponse、12 §1)。 */
export type Notification = {
  id: ID;
  type: NotificationType;
  data: Record<string, unknown>;
  createdAt: Timestamp;
  readAt?: Timestamp;
};

/** ブロックしたユーザー (controller.blockedUserResponse、13 §4)。 */
export type Block = User;

/** エリア (GET/POST /me/areas、07 §4.3、spec-only)。 */
export type Area = {
  id: ID;
  label: string;
  lat: number;
  lon: number;
  radiusM: number;
};

/** フレンド招待 (POST /friends/invites、07 §2.1)。 */
export type Invite = {
  code: string;
  url: string;
  expiresAt: Timestamp;
};

/** 近くのグループ (service.NearbyResult、09 §5)。 */
export type GroupNearbyInside = {
  id: ID;
  name: string;
  memberCount: number;
  isMember: boolean;
  persistent: boolean;
  radiusM: number;
};

export type GroupNearbyNear = {
  id: ID;
  name: string;
  memberCount: number;
  distanceBucket: string;
};

export type GroupNearby = {
  inside: GroupNearbyInside[];
  near: GroupNearbyNear[];
};

/** グループグラフのノード (09 §7、spec-only)。 */
export type GroupGraphNode = {
  id: ID;
  name: string;
  radiusM: number;
  memberCount: number;
  isMember: boolean;
  inside: boolean;
};

/** 辺の種類 (09 §7)。共通メンバーの値は backend の表記に合わせる。 */
export type GroupGraphEdgeKind =
  | "contains"
  | "overlaps"
  | `shared${"_"}members`;

export type GroupGraphEdge = {
  from: string;
  to: string;
  kind: GroupGraphEdgeKind;
  weight?: number;
};

export type GroupGraph = {
  nodes: GroupGraphNode[];
  edges: GroupGraphEdge[];
};

/** instance の Pin (10 §6、lexicon jp.i32.chain.instance.pin、spec-only)。 */
export type Pin = {
  uri: string;
  subject: string;
  reason: "auto" | "manual";
  pinnedBy?: DID;
  createdAt: Timestamp;
};

/** 投稿先 Space (10 §4.1)。group / party はどちらも Space。 */
export type PostTarget = {
  spaceId: string;
  /** Instance.type（"group" / "party"） */
  type: string;
  name: string;
  memberCount: number;
};
