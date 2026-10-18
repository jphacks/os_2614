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
};

/** 位置の見せ方 (07 §4.1)。フレンドごとに設定する。 */
export type LocationPrecision = "exact" | "approx" | "area" | "direction" | "off";

/** フレンド (service.FriendItem、07 §2.3)。 */
export type Friend = User & {
  since: Timestamp;
  precision: LocationPrecision;
};

/** フレンド申請の 1 件 (service.FriendRequestItem、07 §2.2)。 */
export type FriendRequest = {
  id: ID;
  peer: User;
  /** "encounter" | "space" | "invite" など、成立の根拠。 */
  via: string;
  viaRef?: string;
  /** "pending" | "accepted" | "declined" | "canceled" など。 */
  status: string;
  createdAt: Timestamp;
};

/**
 * フレンドの現在位置 (GET /friends/locations、07 §4.2)。相手が自分に見せる精度で
 * 毎回計算された値。stale のときは位置を含まない (30 分以上更新なし)。
 */
export type FriendLocation = {
  did: DID;
  precision: LocationPrecision;
  stale: boolean;
  updatedAt?: Timestamp;
  lat?: number;
  lon?: number;
  accuracyM?: number;
  /** area 精度のときの、相手が今いるエリアの label。 */
  areaLabel?: string;
  /** area 精度で、どの登録エリアにもいない。 */
  outside: boolean;
  /** direction 精度の 8 方位 (N/NE/...)。 */
  direction?: string;
  /** direction 精度の距離帯 (<100m/<500m/<2km/<10km/far)。 */
  distanceBucket?: string;
  /** direction 精度で、見る側 (自分) の位置が古い / 無い。 */
  viewerStale: boolean;
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
  /** すれ違った場所の geohash 6 桁 (06 §7)。記録していなければ undefined。 */
  place?: string;
};

export type GroupVisibility = "open" | "closed";

export type Group = {
  id: ID;
  name: string;
  iconUrl?: string;
  /** 説明文 (service.GroupDetail.description、09 §4)。 */
  description: string;
  visibility: GroupVisibility;
  memberCount: number;
  /** 例: "GPSエリア" */
  areaLabel: string;
  /** エリア内にいるときだけ投稿できる (= backend の isMember) */
  insideArea: boolean;
  /** 閲覧者の役割 (owner / member)。メンバーでなければ undefined (09 §4)。 */
  role?: string;
  /** 招待制のときに必要な承認人数 (09 §4)。 */
  inviteQuorum?: number;
};

/** グループのメンバー (service.MemberItem、09 §4)。プロフィールの要約だけを持つ。 */
export type GroupMember = {
  did: DID;
  handle: string;
  /** 表示名。displayName || handle から導出する */
  name: string;
  avatarUrl?: string;
  tone: AvatarTone;
  /** "owner" | "member"。 */
  role: string;
  joinedAt: Timestamp;
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

/** 投稿本文の形式 (10 §3)。 */
export type PostFormat = "plain" | "markdown";

/** moderation の判定 (11 §1)。 */
export type ModerationLabel = "ok" | "warn" | "block";

/** moderation の表示指示 (11 §4)。backend が label と閲覧者の warnDisplay から決める。 */
export type ModerationDisplay = "show" | "blur" | "hide";

/** 一覧 API の要素に付く moderation の表示指示 (11 §4)。 */
export type PostModeration = {
  label: ModerationLabel;
  display: ModerationDisplay;
  categories: string[];
};

export type Post = {
	id: ID;
	system?: boolean;
  author: User;
  body: string;
  format: PostFormat;
  createdAt: Timestamp;
  attachments: Media[];
  replyCount: number;
  likeCount: number;
  likedByMe: boolean;
  /** 自動ピン (moderation 側で判定) */
  pinned: boolean;
  /** 表示指示。block は hidden で除外され、通常はここに来ない。 */
  moderation?: PostModeration;
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
  /** instance の投稿のときだけ、どの Space の投稿か (17 §3.2)。 */
  space?: FeedSpace;
};

/** フィードの要素が属する Space (17 §3.2)。 */
export type FeedSpace = {
  id: string;
  type: "group" | "party";
  name: string;
};

/** party の段階 (08 §4.2)。 */
export type PartyPhase = "tentative" | "confirmed" | "dissolved" | "ended";

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
  | "group.auto_joined"
  | "group.vote_passed"
  | "group.invite_approval"
  | "encounter.daily"
  | "encounter.weekly"
  | "encounter.monthly"
  | "post.new"
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

/** closed グループの招待 1 件 (service.InviteItem、09 §6)。承認数が要件に達すると offered になる。 */
export type GroupInvite = {
  id: ID;
  targetDid: DID;
  createdBy: DID;
  /** "collecting" | "offered" | "accepted" | "declined" | "expired"。 */
  status: string;
  /** 承認したメンバー数 (招待者の 1 を含む)。 */
  approvedCount: number;
  /** 必要な承認数 min(inviteQuorum, その時点のメンバー数)。 */
  requiredCount: number;
  createdAt: Timestamp;
  expiresAt: Timestamp;
};

/** 自分宛ての offered な招待 1 件 (GET /group-invites、09 §6)。 */
export type GroupInviteInboxItem = {
  id: ID;
  spaceId: string;
  groupName: string;
  inviter?: User;
  expiresAt: Timestamp;
};

/** 投票の種類 (09 §8.6)。closed にする (make_closed) / 常設にする (make_persistent)。 */
export type GroupVoteKind = "make_closed" | "make_persistent";

/** 投票の状態 (backend service.group_vote.go)。open の間だけ投票できる。 */
export type GroupVoteStatus = "open" | "passed" | "failed";

/** グループの投票 1 件 (service.VoteItem、09 §8.6)。 */
export type GroupVote = {
  id: ID;
  kind: GroupVoteKind;
  status: GroupVoteStatus;
  createdBy: DID;
  /** 賛成票の数。 */
  yesCount: number;
  /** 反対票の数。 */
  noCount: number;
  /** 可決に必要な賛成数 (その時点のメンバー数の過半数)。 */
  requiredCount: number;
  createdAt: Timestamp;
  closesAt: Timestamp;
  /** 自分の票。true=賛成 / false=反対 / undefined=未投票。 */
  myBallot?: boolean;
};

/** グループグラフのノード (09 §7)。中心座標は持たない。 */
export type GroupGraphNode = {
  id: ID;
  name: string;
  radiusM: number;
  memberCount: number;
  isMember: boolean;
  inside: boolean;
  origin?: string;
};

/** 辺の種類 (09 §7)。shared_members の weight は共通メンバーの人数。 */
export type GroupGraphEdgeKind = "contains" | "overlaps" | "shared_members";

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
