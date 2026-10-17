// backend (Go/Fiber) の JSON レスポンスの型契約。
// キーはすべて camelCase、時刻は RFC 3339（UTC・ミリ秒まで）。
// 出典は backend の各 controller / service の json タグ。仕様は specification/backend/。
// 任意項目はタグの omitempty に対応して `?` にする。

/** DID（did:plc:... / did:web:...）。API は handle ではなく DID でユーザーを指す。 */
export type DID = string;

/** RFC 3339 の UTC 時刻（例: 2026-10-17T03:12:45.123Z）。 */
export type RFC3339 = string;

/** 通知の種類（12 §3）。 */
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

/** 通知の ON/OFF。既定から変えた種類だけを持つ（12 §3）。 */
export type NotifySettings = Partial<Record<NotificationType, boolean>>;

/** 位置の見せ方（07 §4.1）。 */
export type LocationPrecision =
  | "exact"
  | "approx"
  | "area"
  | "direction"
  | "off";

/** すれ違いの公開モード（06 §5.1）。 */
export type EncounterMode = "immediate" | "delayed" | "private";

/** 相手との関係（06 §6）。 */
export type EncounterRelation = "none" | "requested" | "incoming" | "friend";

/** グループの公開範囲（09 §1）。 */
export type GroupVisibility = "open" | "closed";

/** グループの成り立ち（09 §1）。 */
export type GroupOrigin = "explicit" | "natural";

/** party の段階（08 §4.2）。 */
export type PartyPhase = "tentative" | "confirmed";

/** 投稿本文の形式（10 §3）。 */
export type PostFormat = "plain" | "markdown";

/** moderation 警告の表示方法（11 §1）。 */
export type WarnDisplay = "show" | "blur" | "hide";

/** 共通のページ封筒（02 §4）。次ページがなければ cursor を省略する。 */
export type Page<T> = { items: T[]; cursor?: string };

/** 共通のエラー本文（02 §3）。details はコードごとの補足。 */
export type ApiErrorBody = {
  error: { code: string; message: string; details?: Record<string, unknown> };
};

/** ユーザーの要約（service.UserSummary / AuthorSummary）。 */
export type User = {
  did: DID;
  handle: string;
  displayName?: string;
  avatarUrl?: string;
};

/** GET /users/{did} のプロフィール（13 §1）。 */
export type Profile = User & {
  description?: string;
  bannerUrl?: string;
  status?: string;
};

/** GET /me の設定（controller.meSettingsResponse、06 §5.1 / 07 §4 / 12 §3）。 */
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

/** GET /me（controller.meResponse）。プロフィールと設定を平らに並べる。 */
export type Me = Profile & {
  settings: Settings;
};

/** GET /encounters/summary の集計（06 §6）。 */
export type EncounterSummary = {
  today: number;
  week: number;
  month: number;
  total: number;
  uniquePeople: number;
};

/** GET /encounters の 1 件（service.EncounterItem）。 */
export type Encounter = {
  id: string;
  peer: User;
  seenFrom: RFC3339;
  seenUntil: RFC3339;
  timesWithPeer: number;
  relation: EncounterRelation;
};

/** POST /encounters/sightings の結果。誰とは返らない。 */
export type SightingResult = {
  accepted: number;
  dropped: number;
};

/** メディア（service.MediaItem、10 §4.4）。url / thumbUrl は署名付き URL。 */
export type Media = {
  id: string;
  alt: string;
  url: string;
  thumbUrl: string;
  width: number;
  height: number;
};

/** moderation の表示指示（service.ModerationInfo、11 §4）。 */
export type ModerationInfo = {
  label: string;
  display: string;
  categories?: string[];
};

/** instance の投稿・写真（service.PostItem、10 §3 / §4.1 / §4.2）。 */
export type InstancePost = {
  kind: string;
  uri: string;
  spaceId: string;
  author?: User;
  text?: string;
  format?: PostFormat;
  /** 返信の参照（service.ReplyRef、10 §3）。返信でなければ無い。 */
  reply?: FeedReplyRef;
  media?: Media[];
  moderation?: ModerationInfo;
  hidden?: string;
  takenAt?: RFC3339;
  createdAt: RFC3339;
};

/** GET /instances の 1 件（controller.instanceItem、13 §2）。 */
export type Instance = {
  spaceId: string;
  type: string;
  name: string;
  state: string;
  memberCount: number;
  lastPostAt?: RFC3339;
  archivedAt?: RFC3339;
};

/** POST /posts の 1 件（service.CreatedPost）。 */
export type CreatedPost = {
  uri: string;
  spaceId: string;
};

/** POST /posts と同時に作る公開投稿の結果（service.PublicPostOutcome、10 §4.1）。 */
export type PublicPostOutcome = {
  uri?: string;
  cid?: string;
  bskyUri?: string;
  /** PDS への公開投稿に失敗したときの API コード。instance 投稿は成功している。 */
  error?: string;
};

/** POST /posts の応答（service.CreatePostResult、10 §4.1）。 */
export type CreatePostResult = {
  items: CreatedPost[];
  /** alsoPublic:true のときだけ入る。失敗しても items は成功している。 */
  public?: PublicPostOutcome;
};

/** POST /media の応答（service.MediaUpload、10 §4.4）。 */
export type MediaUpload = {
  id: string;
  width: number;
  height: number;
};

/** GET /friends の 1 件（service.FriendItem、07 §2.3）。 */
export type Friend = User & {
  since: RFC3339;
  precision: LocationPrecision;
};

/**
 * GET /friends/locations の 1 件（controller.friendLocationResponse、07 §4.2）。
 * precision ごとに含まれる項目が変わる。stale のときは位置を返さない。
 */
export type FriendLocation = {
  did: DID;
  precision: LocationPrecision;
  stale?: boolean;
  updatedAt?: RFC3339;
  lat?: number;
  lon?: number;
  accuracyM?: number;
  area?: { label: string } | null;
  outside?: boolean;
  direction?: string;
  distanceBucket?: string;
  viewerStale?: boolean;
};

/** フレンド申請の 1 件（service.FriendRequestItem、07 §2.2）。 */
export type FriendRequest = {
  id: string;
  peer: User;
  via: string;
  viaRef?: string;
  status: string;
  createdAt: RFC3339;
};

/** POST /friends/invites の応答（07 §2.1）。 */
export type Invite = {
  code: string;
  url: string;
  expiresAt: RFC3339;
};

/** beacon token の 1 窓（controller.beaconTokenResponse、05 §2.2）。 */
export type BeaconToken = {
  windowStart: RFC3339;
  token: string;
};

/** GET /beacon/tokens の応答（controller.beaconTokensResponse）。 */
export type BeaconTokens = {
  windowSec: number;
  tokens: BeaconToken[];
};

/** フレンドの beacon token（controller.friendBeaconResponse、08 §2）。 */
export type FriendBeacon = {
  did: DID;
  tokens: BeaconToken[];
};

/** GET /party/friend-beacons の応答（controller.friendBeaconsResponse、08 §2）。 */
export type FriendBeacons = {
  validUntil: RFC3339;
  friends: FriendBeacon[];
};

/** party のメンバー（service.PartyMemberItem、08 §6）。 */
export type PartyMember = {
  did: DID;
  handle?: string;
  displayName?: string;
  avatarUrl?: string;
  present: boolean;
  left: boolean;
};

/** GET /party/current の応答（service.PartyDetail、08 §6）。 */
export type Party = {
  id: string;
  name: string;
  phase: PartyPhase;
  locked: boolean;
  tentativeAt: RFC3339;
  confirmedAt?: RFC3339;
  members: PartyMember[];
};

/** グループの有効時間帯（service.GroupSchedule、09 §3）。weekday は 1=月〜7=日。 */
export type GroupScheduleSlot = { weekday: number; start: string; end: string };
export type GroupSchedule = { tz: string; slots: GroupScheduleSlot[] };

/** POST /groups の geofence（controller.geofenceRequest、09 §2）。 */
export type Geofence = { lat: number; lon: number; radiusM: number };

/** POST /groups の入力（controller.createGroupRequest、09 §2）。 */
export type GroupCreateRequest = {
  name: string;
  description: string;
  visibility: GroupVisibility;
  persistent: boolean;
  geofence: Geofence;
  schedule?: GroupSchedule;
  inviteQuorum: number;
  force: boolean;
};

/** GET /groups/{id}（service.GroupDetail、09 §4）。中心座標はメンバーのときだけ入る。 */
export type Group = {
  id: string;
  name: string;
  description: string;
  visibility: GroupVisibility;
  memberCount: number;
  isMember: boolean;
  state?: string;
  createdAt?: RFC3339;
  role?: string;
  origin?: GroupOrigin;
  persistent?: boolean;
  radiusM?: number;
  centerLat?: number;
  centerLon?: number;
  schedule?: GroupSchedule;
  inviteQuorum?: number;
  parentSpaceId?: string;
};

/** GET /groups/{id}/members の 1 件（service.MemberItem、09 §4）。 */
export type GroupMember = {
  did: DID;
  handle: string;
  displayName?: string;
  avatarUrl?: string;
  role: string;
  joinedAt: RFC3339;
};

/** GET /groups/nearby（service.NearbyResult、09 §5）。 */
export type GroupNearbyInside = {
  id: string;
  name: string;
  memberCount: number;
  isMember: boolean;
  persistent: boolean;
  radiusM: number;
};

export type GroupNearbyNear = {
  id: string;
  name: string;
  memberCount: number;
  distanceBucket: string;
};

export type GroupNearby = {
  inside: GroupNearbyInside[];
  near: GroupNearbyNear[];
};

/** memory の表紙（service.MemoryCover、13 §2）。 */
export type MemoryCover = { thumbUrl: string };

/** GET /memories の 1 件（service.MemoryItem、13 §2）。 */
export type Memory = {
  spaceId: string;
  type: string;
  name: string;
  from: RFC3339;
  until: RFC3339;
  memberCount: number;
  mediaCount: number;
  cover?: MemoryCover;
};

/** GET /notifications の 1 件（controller.notificationResponse、12 §1）。 */
export type Notification = {
  id: string;
  type: NotificationType;
  data: Record<string, unknown>;
  createdAt: RFC3339;
  readAt?: RFC3339;
};

/** GET /blocks の 1 件（controller.blockedUserResponse、13 §4）。 */
export type Block = User;

/** PUT /me/consents の応答（controller.consentResponse、02 §8）。 */
export type Consent = {
  contentScan: number;
  consentedAt: string | null;
};

/** GET /me/areas の 1 件（controller.areaResponse、07 §4.3）。 */
export type Area = {
  id: string;
  label: string;
  lat: number;
  lon: number;
  radiusM: number;
  createdAt: RFC3339;
};

/**
 * POST /me/areas の入力（controller.areaCreateRequest、07 §4.3）。
 * label は 20 文字まで、radiusM は 50〜5000 m、1 ユーザー 10 個まで。
 */
export type AreaCreateRequest = {
  label: string;
  lat: number;
  lon: number;
  radiusM: number;
};

/** 公開投稿が属する Space（service.FeedSpace、17 §3.2）。 */
export type FeedSpace = { id: string; type: string; name: string };

/** 公開投稿の返信参照（service.ReplyRef、10 §3）。 */
export type FeedReplyRef = { root: string; parent: string };

/** GET /feed の 1 件（service.FeedItem、17 §3.2）。source=user:{did} で自分の投稿を取る。 */
export type FeedItem = {
  kind: string;
  uri: string;
  space?: FeedSpace;
  author?: User;
  text?: string;
  format?: PostFormat;
  reply?: FeedReplyRef;
  system?: boolean;
  media?: Media[];
  moderation?: ModerationInfo;
  hidden?: string;
  takenAt?: RFC3339;
  createdAt: RFC3339;
};

// --- ここから下は backend 未実装（spec-only）の形。実装されたら json タグに合わせて直す。 ---

/** GET /me/stats（13 §3）。未実装（spec-only）。 */
export type Stats = {
  encounters: EncounterSummary;
  friends: number;
  parties: { total: number };
  groups: { member: number };
  posts: { month: number };
};

/** GET /groups/graph（09 §7）。未実装（spec-only）。中心座標は返さない。 */
export type GroupGraphNode = {
  id: string;
  name: string;
  radiusM: number;
  memberCount: number;
  isMember: boolean;
  inside: boolean;
};

/** 辺の種類（09 §7）。共通メンバーの値は backend の表記に合わせる。 */
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

/** instance の Pin（10 §6、lexicon jp.i32.chain.instance.pin）。未実装（spec-only）。 */
export type Pin = {
  uri: string;
  subject: string;
  reason: "auto" | "manual";
  pinnedBy?: DID;
  createdAt: RFC3339;
};

/** POST /reports の入力（11 §6）。未実装（spec-only）。 */
export type ReportRequest = {
  uri: string;
  reason: string;
};
