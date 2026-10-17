// UI が依存するドメイン型。backend の DTO とは src/data/mappers.ts で変換する。
// backend 未確定のため、画面に必要な最小限のフィールドだけを持つ。

export type ID = string;
/** ISO 8601 文字列 */
export type Timestamp = string;

/** アバター背景のパステル色。ユーザー ID から決定的に選ぶ (avatarToneFor) */
export type AvatarTone =
  | "peach"
  | "blue"
  | "green"
  | "lavender"
  | "mint"
  | "yellow"
  | "periwinkle"
  | "sand";

export type User = {
  id: ID;
  name: string;
  /** 画像が無いときは name の先頭 1 文字を表示する */
  avatarUrl?: string;
  tone: AvatarTone;
};

export type Profile = User & {
  bio: string;
  /** 例: "図書館でもくもく中" */
  status?: string;
  bannerUrl?: string;
  counts: { encounters: number; friends: number; parties: number };
};

/** フレンドの位置の見せ方 */
export type LocationVisibility = "area" | "direction" | "hidden";

export type Friend = User & {
  /** 同じパーティ / 近くにいる */
  together: boolean;
  /** 例: "パーティ中・大学キャンパス" / "渋谷エリア" / "北東のほう"。hidden のときは undefined */
  locationLabel?: string;
  locationVisibility: LocationVisibility;
  lastSeenAt?: Timestamp;
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

export type Attachment = {
  id: ID;
  kind: "image";
  url: string;
  /** 投稿者。パーティの写真グリッドで右下に色点として出す */
  by?: User;
};

export type Post = {
  id: ID;
  author: User;
  body: string;
  createdAt: Timestamp;
  attachments: Attachment[];
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

export type PartyStage = "provisional" | "active";

export type Party = {
  id: ID;
  /** 例: "渋谷エリア" */
  areaLabel: string;
  stage: PartyStage;
  startedAt: Timestamp;
  /** 固定するとはぐれても解散しない */
  fixed: boolean;
  members: (User & { viaFriend: boolean })[];
  photos: Attachment[];
  photoCount: number;
};

export type Memory = {
  id: ID;
  title: string;
  startedAt: Timestamp;
  endedAt: Timestamp;
  memberCount: number;
  members: User[];
  photos: Attachment[];
  photoCount: number;
};

export type StatsRange = "week" | "month" | "year" | "all";

export type Stats = {
  range: StatsRange;
  encounters: {
    total: number;
    /** 前期間との差 */
    delta: number;
    /** 古い順。最後の要素が今日 */
    series: { label: string; value: number }[];
  };
  friends: { total: number; delta: number };
  parties: { total: number; totalHours: number };
  peakHourLabel: string;
  peakHourNote: string;
  topArea: { label: string; ratio: number };
  /** 直近 12 週 × 7 日, 0–4 の段階 */
  activity: number[][];
  messagesThisMonth: number;
};

/** すれ違いを相手が見られるまでの時間。null は非公開 */
export type RevealDelay = { hours: number } | null;

export type Settings = {
  revealDelay: RevealDelay;
  allowFriendRequests: boolean;
  locationVisibility: LocationVisibility;
  /** 決めたエリアだけで共有 */
  restrictToAreas: boolean;
  sharedAreas: string[];
};

/** 投稿先 (グループ or 参加中のパーティ) */
export type PostTarget =
  | { kind: "group"; id: ID; name: string; memberCount: number }
  | { kind: "party"; id: ID; name: string };
