// 画面確認用のモック。ドメイン型 (src/domain/types.ts) の値だけを持つ。
// backend ができたら app/*/page.tsx が src/data/api.ts に差し替える。

import type {
  Friend,
  Group,
  Media,
  Memory,
  Party,
  PartyMember,
  Post,
  PostTarget,
  Profile,
  Reply,
  Settings,
  Stats,
  User,
} from "@/domain/types";
import { avatarToneFor } from "./mappers";

function createUser(
  did: string,
  handle: string,
  name: string,
  avatarUrl?: string,
): User {
  return { did, handle, name, avatarUrl, tone: avatarToneFor(did) };
}

function createMedia(id: string, url = ""): Media {
  return { id, alt: "", url, thumbUrl: url, width: 1600, height: 1200 };
}

function partyMember(u: User, present = true, left = false): PartyMember {
  return {
    did: u.did,
    handle: u.handle,
    displayName: u.name,
    avatarUrl: u.avatarUrl,
    present,
    left,
  };
}

export const yui = createUser("did:plc:yui0000000000", "yui.bsky.social", "ゆい");
export const kenta = createUser("did:plc:kenta00000000", "kenta.bsky.social", "Kenta");
export const misaki = createUser("did:plc:misaki0000000", "misaki.bsky.social", "みさき");
export const takumi = createUser("did:plc:takumi0000000", "takumi.bsky.social", "たくみ");
export const sota = createUser("did:plc:sota000000000", "sota.bsky.social", "そうた");
export const nana = createUser("did:plc:nana000000000", "nana.bsky.social", "なな");
export const sho = createUser("did:plc:sho0000000000", "sho.bsky.social", "しょう");
export const haru = createUser("did:plc:haru000000000", "haru.bsky.social", "はる");

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
  { ...kenta, since: "2026-05-01T10:00:00Z", precision: "area" },
  { ...misaki, since: "2026-05-01T10:00:00Z", precision: "area" },
  { ...takumi, since: "2026-06-12T10:00:00Z", precision: "direction" },
  { ...sota, since: "2026-04-20T10:00:00Z", precision: "area" },
  { ...nana, since: "2026-07-03T10:00:00Z", precision: "off" },
  { ...sho, since: "2026-03-15T10:00:00Z", precision: "area" },
  { ...haru, since: "2026-08-21T10:00:00Z", precision: "direction" },
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
    format: "plain",
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
    format: "plain",
    createdAt: "2026-10-17T09:12:00+09:00",
    attachments: [createMedia("a1")],
    replyCount: 3,
    likeCount: 5,
    likedByMe: false,
    pinned: false,
  },
  {
    id: "p3",
    author: takumi,
    body: "学食の新しいメニュー、思ったよりうまかった。",
    format: "plain",
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
  name: "大学キャンパスパーティ",
  phase: "confirmed",
  locked: false,
  // 画面確認用に、表示時点の 42 分 18 秒前から始まったことにする
  tentativeAt: new Date(Date.now() - (42 * 60 + 18) * 1000).toISOString(),
  confirmedAt: new Date(Date.now() - 30 * 60 * 1000).toISOString(),
  members: [
    partyMember(kenta),
    partyMember(misaki),
    partyMember(takumi),
    partyMember(sota),
  ],
};

export const memories: Memory[] = [
  {
    spaceId: "mem-kyoto",
    type: "party",
    name: "京都旅行",
    from: "2026-10-03T10:00:00Z",
    until: "2026-10-04T18:00:00Z",
    memberCount: 5,
    mediaCount: 12,
    cover: { thumbUrl: "" },
  },
];

export const stats: Stats = {
  encounters: { today: 7, week: 42, month: 248, total: 1204, uniquePeople: 96 },
  friends: 60,
  parties: { total: 12 },
  groups: { member: 8 },
  posts: { month: 812 },
};

export const settings: Settings = {
  encounterMode: "delayed",
  encounterDelaySec: 10800,
  friendBeaconShare: true,
  partyAuto: false,
  locationPaused: false,
  warnDisplay: "blur",
  notify: { "encounter.daily": true, "friend.request": true },
};

export const postTargets: PostTarget[] = [
  { spaceId: "g1", type: "group", name: "情報工学科 3年", memberCount: 8 },
  {
    spaceId: "party-1",
    type: "party",
    name: "大学キャンパスパーティ",
    memberCount: 4,
  },
];
