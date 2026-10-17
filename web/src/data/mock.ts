// 画面確認用のモック。ドメイン型 (src/domain/types.ts) の値だけを持つ。
// backend ができたら app/*/page.tsx が src/data/api.ts に差し替える。

import type {
  Attachment,
  Friend,
  Group,
  Memory,
  Party,
  Post,
  PostTarget,
  Profile,
  Reply,
  Settings,
  Stats,
  User,
} from "@/domain/types";
import { avatarToneFor } from "./mappers";

function createUser(id: string, name: string, avatarUrl?: string): User {
  return { id, name, avatarUrl, tone: avatarToneFor(id) };
}

function createPhoto(
  id: string,
  by: User,
  url = "",
): Attachment {
  return { id, kind: "image", url, by };
}

export const yui = createUser("u-yui", "ゆい");
export const kenta = createUser("u-kenta", "Kenta");
export const misaki = createUser("u-misaki", "みさき");
export const takumi = createUser("u-takumi", "たくみ");
export const sota = createUser("u-sota", "そうた");
export const nana = createUser("u-nana", "なな");
export const sho = createUser("u-sho", "しょう");
export const haru = createUser("u-haru", "はる");

export const users: User[] = [
  yui,
  kenta,
  misaki,
  takumi,
  sota,
  nana,
  sho,
  haru,
];

export const me: Profile = {
  ...yui,
  bio: "情報工学科3年。京都と写真が好き。",
  status: "図書館でもくもく中",
  counts: { encounters: 248, friends: 60, parties: 12 },
};

export const friends: Friend[] = [
  {
    ...kenta,
    together: true,
    locationLabel: "パーティ中・大学キャンパス",
    locationVisibility: "area",
    lastSeenAt: "2026-10-17T13:58:00+09:00",
  },
  {
    ...misaki,
    together: true,
    locationLabel: "渋谷エリア",
    locationVisibility: "area",
    lastSeenAt: "2026-10-17T13:55:00+09:00",
  },
  {
    ...takumi,
    together: false,
    locationLabel: "北東のほう",
    locationVisibility: "direction",
    lastSeenAt: "2026-10-17T12:20:00+09:00",
  },
  {
    ...sota,
    together: false,
    locationLabel: "京都駅エリア",
    locationVisibility: "area",
    lastSeenAt: "2026-10-17T09:05:00+09:00",
  },
  {
    ...nana,
    together: false,
    locationVisibility: "hidden",
  },
  {
    ...sho,
    together: false,
    locationLabel: "大学キャンパス",
    locationVisibility: "area",
    lastSeenAt: "2026-10-16T19:40:00+09:00",
  },
  {
    ...haru,
    together: false,
    locationLabel: "南のほう",
    locationVisibility: "direction",
    lastSeenAt: "2026-10-15T21:10:00+09:00",
  },
];

export const groupInfoKeisanden: Group = {
  id: "g1",
  name: "情報工学科 3年",
  visibility: "open",
  memberCount: 8,
  areaLabel: "GPSエリア",
  insideArea: true,
};

export const groups: Group[] = [groupInfoKeisanden];

export const posts: Post[] = [
  {
    id: "p1",
    author: kenta,
    body: "明日の集合は10時、正門前で！遅れる人は連絡ください。",
    createdAt: "2026-10-17T11:30:00+09:00",
    attachments: [],
    replyCount: 2,
    likeCount: 4,
    likedByMe: false,
    pinned: true,
  },
  {
    id: "p2",
    author: misaki,
    body: "レポート終わった人いる？図書館で見せ合いっこしたい。",
    createdAt: "2026-10-17T09:12:00+09:00",
    attachments: [createPhoto("a1", misaki)],
    replyCount: 3,
    likeCount: 5,
    likedByMe: false,
    pinned: false,
  },
  {
    id: "p3",
    author: takumi,
    body: "学食の新しいメニュー、思ったよりうまかった。",
    createdAt: "2026-10-16T12:40:00+09:00",
    attachments: [],
    replyCount: 0,
    likeCount: 2,
    likedByMe: true,
    pinned: false,
  },
];

export const replies: Reply[] = [
  {
    id: "r1",
    author: sota,
    body: "終わった！15時くらいに図書館行くよ。",
    createdAt: "2026-10-17T09:20:00+09:00",
    likeCount: 1,
    likedByMe: false,
  },
  {
    id: "r2",
    author: nana,
    body: "わたしも混ぜて。3階の席とっておくね。",
    createdAt: "2026-10-17T09:34:00+09:00",
    likeCount: 2,
    likedByMe: true,
  },
];

export const party: Party = {
  id: "party-1",
  areaLabel: "渋谷エリア",
  stage: "active",
  // 画面確認用に、表示時点の 42 分 18 秒前から始まったことにする
  startedAt: new Date(Date.now() - (42 * 60 + 18) * 1000).toISOString(),
  fixed: false,
  members: [
    { ...kenta, viaFriend: false },
    { ...misaki, viaFriend: false },
    { ...takumi, viaFriend: true },
    { ...sota, viaFriend: true },
  ],
  photos: [
    createPhoto("pp1", kenta),
    createPhoto("pp2", misaki),
    createPhoto("pp3", takumi),
    createPhoto("pp4", kenta),
    createPhoto("pp5", sota),
    createPhoto("pp6", misaki),
  ],
  photoCount: 9,
};

export const memories: Memory[] = [
  {
    id: "m1",
    title: "京都旅行",
    startedAt: "2026-10-03T10:00:00+09:00",
    endedAt: "2026-10-04T18:00:00+09:00",
    memberCount: 5,
    members: [yui, kenta, misaki, takumi, sota],
    photos: [
      createPhoto("mp1", kenta),
      createPhoto("mp2", misaki),
      createPhoto("mp3", takumi),
      createPhoto("mp4", yui),
    ],
    photoCount: 12,
  },
];

const activity: number[][] = Array.from({ length: 12 }, (_, week) =>
  Array.from({ length: 7 }, (_, day) => (week + day * 2) % 5),
);

export const stats: Stats = {
  range: "month",
  encounters: {
    total: 248,
    delta: 38,
    series: [
      { label: "10/11", value: 12 },
      { label: "10/12", value: 18 },
      { label: "10/13", value: 9 },
      { label: "10/14", value: 22 },
      { label: "10/15", value: 16 },
      { label: "10/16", value: 27 },
      { label: "10/17", value: 31 },
    ],
  },
  friends: { total: 60, delta: 4 },
  parties: { total: 12, totalHours: 34 },
  peakHourLabel: "18時ごろ",
  peakHourNote: "授業終わりに多い",
  topArea: { label: "大学キャンパス", ratio: 60 },
  activity,
  messagesThisMonth: 812,
};

export const settings: Settings = {
  revealDelay: { hours: 3 },
  allowFriendRequests: true,
  locationVisibility: "area",
  restrictToAreas: true,
  sharedAreas: ["大学キャンパス", "渋谷エリア"],
};

export const postTargets: PostTarget[] = [
  { kind: "group", id: "g1", name: "情報工学科 3年", memberCount: 8 },
  { kind: "party", id: "party-1", name: "パーティ" },
];
