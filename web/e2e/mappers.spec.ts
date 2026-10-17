import { expect, test } from "@playwright/test";

import {
  avatarToneFor,
  fromSettings,
  toEncounter,
  toFriend,
  toGroup,
  toMedia,
  toMemory,
  toParty,
  toPin,
  toPost,
  toProfile,
  toReply,
  toSettings,
  toStats,
  toUser,
} from "../src/data/mappers";

// 純関数のテスト。ブラウザもサーバーも使わないので決定的に速く回る。
// 各 mapper に仕様どおりの JSON（06 §6 / 13 §2 / 13 §3 / service.PostItem /
// service.PartyDetail）をそのまま通し、欠損・null・{} でも例外を投げず
// 既定値になることを確かめる。

test.describe("avatarToneFor", () => {
  test("is deterministic for the same DID", () => {
    const tone = avatarToneFor("did:plc:yui0000000000");
    expect(avatarToneFor("did:plc:yui0000000000")).toBe(tone);
    expect([
      "peach",
      "blue",
      "green",
      "lavender",
      "mint",
      "yellow",
      "periwinkle",
      "sand",
    ]).toContain(tone);
  });

  test("falls back to the first tone for an empty input", () => {
    expect(avatarToneFor("")).toBe("peach");
  });
});

test.describe("toUser", () => {
  test("derives name from displayName and tone from the DID", () => {
    const user = toUser({
      did: "did:plc:bob",
      handle: "bob.bsky.social",
      displayName: "Bob",
      avatarUrl: "https://cdn.example/bob.jpg",
    });
    expect(user).toEqual({
      did: "did:plc:bob",
      handle: "bob.bsky.social",
      name: "Bob",
      avatarUrl: "https://cdn.example/bob.jpg",
      tone: avatarToneFor("did:plc:bob"),
    });
  });

  test("falls back to the handle when displayName is missing", () => {
    expect(toUser({ did: "did:plc:bob", handle: "bob.bsky.social" }).name).toBe(
      "bob.bsky.social",
    );
  });

  test("never throws on null and fills defaults", () => {
    expect(toUser(null)).toEqual({
      did: "",
      handle: "",
      name: "",
      tone: "peach",
    });
  });
});

test.describe("toProfile", () => {
  test("maps the flat GET /me profile fields", () => {
    const profile = toProfile({
      did: "did:plc:yui",
      handle: "yui.bsky.social",
      displayName: "ゆい",
      description: "京都と写真が好き。",
      avatarUrl: "https://cdn.example/yui.jpg",
      bannerUrl: "https://cdn.example/banner.jpg",
      status: "図書館でもくもく中",
    });
    expect(profile).toEqual({
      did: "did:plc:yui",
      handle: "yui.bsky.social",
      name: "ゆい",
      avatarUrl: "https://cdn.example/yui.jpg",
      tone: avatarToneFor("did:plc:yui"),
      bio: "京都と写真が好き。",
      status: "図書館でもくもく中",
      bannerUrl: "https://cdn.example/banner.jpg",
      counts: { encounters: 0, friends: 0, parties: 0 },
    });
  });

  test("never throws on {} and fills defaults", () => {
    const profile = toProfile({});
    expect(profile.bio).toBe("");
    expect(profile.counts).toEqual({ encounters: 0, friends: 0, parties: 0 });
  });
});

test.describe("toFriend", () => {
  test("maps since and precision (07 §2.3)", () => {
    expect(
      toFriend({
        did: "did:plc:kenta",
        handle: "kenta.bsky.social",
        displayName: "Kenta",
        since: "2026-05-01T10:00:00Z",
        precision: "direction",
      }),
    ).toEqual({
      did: "did:plc:kenta",
      handle: "kenta.bsky.social",
      name: "Kenta",
      tone: avatarToneFor("did:plc:kenta"),
      since: "2026-05-01T10:00:00Z",
      precision: "direction",
    });
  });

  test("defaults a missing precision to area", () => {
    expect(toFriend({ did: "did:plc:k", handle: "k" }).precision).toBe("area");
  });

  test("never throws on null and fills defaults", () => {
    expect(toFriend(null)).toEqual({
      did: "",
      handle: "",
      name: "",
      tone: "peach",
      since: "",
      precision: "area",
    });
  });
});

test.describe("toEncounter", () => {
  test("maps the 06 §6 sample", () => {
    const encounter = toEncounter({
      id: "0192abcdef",
      peer: {
        did: "did:plc:bob",
        handle: "bob.bsky.social",
        displayName: "Bob",
        avatarUrl: "https://cdn.example/bob.jpg",
      },
      seenFrom: "2026-10-17T03:00:00Z",
      seenUntil: "2026-10-17T03:15:00Z",
      timesWithPeer: 4,
      relation: "none",
    });
    expect(encounter).toEqual({
      id: "0192abcdef",
      peer: {
        did: "did:plc:bob",
        handle: "bob.bsky.social",
        name: "Bob",
        avatarUrl: "https://cdn.example/bob.jpg",
        tone: avatarToneFor("did:plc:bob"),
      },
      seenFrom: "2026-10-17T03:00:00Z",
      seenUntil: "2026-10-17T03:15:00Z",
      timesWithPeer: 4,
      relation: "none",
    });
  });

  test("defaults a missing relation to none", () => {
    expect(toEncounter({ id: "x", peer: { did: "d", handle: "h" } }).relation).toBe(
      "none",
    );
  });

  test("never throws on null and fills defaults", () => {
    expect(toEncounter(null)).toEqual({
      id: "",
      peer: { did: "", handle: "", name: "", tone: "peach" },
      seenFrom: "",
      seenUntil: "",
      timesWithPeer: 0,
      relation: "none",
    });
  });
});

test.describe("toGroup", () => {
  test("maps a geofenced group to insideArea + area label", () => {
    expect(
      toGroup({
        id: "g1",
        name: "情報工学科 3年",
        description: "",
        visibility: "open",
        memberCount: 8,
        isMember: true,
        radiusM: 100,
      }),
    ).toEqual({
      id: "g1",
      name: "情報工学科 3年",
      visibility: "open",
      memberCount: 8,
      areaLabel: "GPSエリア",
      insideArea: true,
    });
  });

  test("leaves the area label empty when there is no radius", () => {
    const group = toGroup({
      id: "g2",
      name: "サークル",
      description: "",
      visibility: "closed",
      memberCount: 3,
      isMember: false,
    });
    expect(group.areaLabel).toBe("");
    expect(group.insideArea).toBe(false);
  });

  test("never throws on null and fills defaults", () => {
    expect(toGroup(null)).toEqual({
      id: "",
      name: "",
      visibility: "open",
      memberCount: 0,
      areaLabel: "",
      insideArea: false,
    });
  });
});

test.describe("toMedia", () => {
  test("maps the MediaItem fields (10 §4.4)", () => {
    expect(
      toMedia({
        id: "m1",
        alt: "集合写真",
        url: "https://chain.i32.jp/media/m1?v=full",
        thumbUrl: "https://chain.i32.jp/media/m1?v=thumb",
        width: 2048,
        height: 1536,
      }),
    ).toEqual({
      id: "m1",
      alt: "集合写真",
      url: "https://chain.i32.jp/media/m1?v=full",
      thumbUrl: "https://chain.i32.jp/media/m1?v=thumb",
      width: 2048,
      height: 1536,
    });
  });

  test("never throws on null and fills defaults", () => {
    expect(toMedia(null)).toEqual({
      id: "",
      alt: "",
      url: "",
      thumbUrl: "",
      width: 0,
      height: 0,
    });
  });
});

test.describe("toPost", () => {
  test("maps the service.PostItem sample", () => {
    expect(
      toPost({
        kind: "post",
        uri: "at://did:plc:kenta/jp.i32.chain.instance.post/1",
        spaceId: "g1",
        author: {
          did: "did:plc:kenta",
          handle: "kenta.bsky.social",
          displayName: "Kenta",
          avatarUrl: "https://cdn.example/kenta.jpg",
        },
        text: "明日の集合は10時、正門前で！",
        format: "plain",
        media: [
          {
            id: "m1",
            alt: "",
            url: "https://chain.i32.jp/media/m1?v=full",
            thumbUrl: "https://chain.i32.jp/media/m1?v=thumb",
            width: 2048,
            height: 1536,
          },
        ],
        createdAt: "2026-10-17T02:30:00Z",
      }),
    ).toEqual({
      id: "at://did:plc:kenta/jp.i32.chain.instance.post/1",
      author: {
        did: "did:plc:kenta",
        handle: "kenta.bsky.social",
        name: "Kenta",
        avatarUrl: "https://cdn.example/kenta.jpg",
        tone: avatarToneFor("did:plc:kenta"),
      },
      body: "明日の集合は10時、正門前で！",
      format: "plain",
      createdAt: "2026-10-17T02:30:00Z",
      attachments: [
        {
          id: "m1",
          alt: "",
          url: "https://chain.i32.jp/media/m1?v=full",
          thumbUrl: "https://chain.i32.jp/media/m1?v=thumb",
          width: 2048,
          height: 1536,
        },
      ],
      replyCount: 0,
      likeCount: 0,
      likedByMe: false,
      pinned: false,
    });
  });

  test("does not treat a moderation-labelled post as pinned", () => {
    expect(
      toPost({
        kind: "post",
        uri: "at://x/1",
        spaceId: "g1",
        text: "flagged",
        createdAt: "2026-10-17T02:30:00Z",
        moderation: { label: "warn", display: "blur" },
      }).pinned,
    ).toBe(false);
  });

  test("uses an empty author when author is missing (hidden post)", () => {
    const post = toPost({
      kind: "post",
      uri: "at://x/2",
      spaceId: "g1",
      text: "hidden",
      hidden: "moderation",
      createdAt: "2026-10-17T02:30:00Z",
    });
    expect(post.author).toEqual({
      did: "",
      handle: "",
      name: "",
      tone: "peach",
    });
    expect(post.attachments).toEqual([]);
  });

  test("never throws on null and fills defaults", () => {
    expect(toPost(null)).toEqual({
      id: "",
      author: { did: "", handle: "", name: "", tone: "peach" },
      body: "",
      format: "plain",
      createdAt: "",
      attachments: [],
      replyCount: 0,
      likeCount: 0,
      likedByMe: false,
      pinned: false,
    });
  });
});

test.describe("toReply", () => {
  test("maps a reply-shaped PostItem", () => {
    const reply = toReply({
      kind: "post",
      uri: "at://x/reply/1",
      spaceId: "g1",
      author: { did: "did:plc:sota", handle: "sota.bsky.social", displayName: "そうた" },
      text: "終わった！15時くらいに図書館行くよ。",
      createdAt: "2026-10-17T00:20:00Z",
    });
    expect(reply).toEqual({
      id: "at://x/reply/1",
      author: {
        did: "did:plc:sota",
        handle: "sota.bsky.social",
        name: "そうた",
        tone: avatarToneFor("did:plc:sota"),
      },
      body: "終わった！15時くらいに図書館行くよ。",
      createdAt: "2026-10-17T00:20:00Z",
      likeCount: 0,
      likedByMe: false,
    });
  });

  test("never throws on null and fills defaults", () => {
    expect(toReply(null)).toEqual({
      id: "",
      author: { did: "", handle: "", name: "", tone: "peach" },
      body: "",
      createdAt: "",
      likeCount: 0,
      likedByMe: false,
    });
  });
});

test.describe("toParty", () => {
  test("maps the service.PartyDetail sample (08 §6)", () => {
    expect(
      toParty({
        id: "party-1",
        name: "大学キャンパスパーティ",
        phase: "confirmed",
        locked: false,
        tentativeAt: "2026-10-17T00:00:00Z",
        confirmedAt: "2026-10-17T00:12:00Z",
        members: [
          {
            did: "did:plc:kenta",
            handle: "kenta.bsky.social",
            displayName: "Kenta",
            avatarUrl: "https://cdn.example/kenta.jpg",
            present: true,
            left: false,
          },
          { did: "did:plc:sota", handle: "sota.bsky.social", present: false, left: true },
        ],
      }),
    ).toEqual({
      id: "party-1",
      name: "大学キャンパスパーティ",
      phase: "confirmed",
      locked: false,
      tentativeAt: "2026-10-17T00:00:00Z",
      confirmedAt: "2026-10-17T00:12:00Z",
      members: [
        {
          did: "did:plc:kenta",
          handle: "kenta.bsky.social",
          displayName: "Kenta",
          avatarUrl: "https://cdn.example/kenta.jpg",
          present: true,
          left: false,
        },
        {
          did: "did:plc:sota",
          handle: "sota.bsky.social",
          present: false,
          left: true,
        },
      ],
    });
  });

  test("defaults member present/left to false when missing", () => {
    const party = toParty({
      id: "p",
      name: "n",
      phase: "tentative",
      locked: true,
      tentativeAt: "2026-10-17T00:00:00Z",
      members: [{ did: "did:plc:x" }],
    });
    expect(party.members[0]).toEqual({
      did: "did:plc:x",
      present: false,
      left: false,
    });
  });

  test("never throws on null and fills defaults", () => {
    expect(toParty(null)).toEqual({
      id: "",
      name: "",
      phase: "tentative",
      locked: false,
      tentativeAt: "",
      members: [],
    });
  });
});

test.describe("toMemory", () => {
  test("maps the 13 §2 sample", () => {
    expect(
      toMemory({
        spaceId: "mem-kyoto",
        type: "party",
        name: "10/17 13:05 の party",
        from: "2026-10-17T04:05:00Z",
        until: "2026-10-17T09:40:00Z",
        memberCount: 4,
        mediaCount: 37,
        cover: { thumbUrl: "https://chain.i32.jp/media/cover?v=thumb" },
      }),
    ).toEqual({
      spaceId: "mem-kyoto",
      type: "party",
      name: "10/17 13:05 の party",
      from: "2026-10-17T04:05:00Z",
      until: "2026-10-17T09:40:00Z",
      memberCount: 4,
      mediaCount: 37,
      cover: { thumbUrl: "https://chain.i32.jp/media/cover?v=thumb" },
    });
  });

  test("leaves cover undefined when there is none", () => {
    const memory = toMemory({
      spaceId: "s",
      type: "group",
      name: "n",
      from: "2026-01-01T00:00:00Z",
      until: "2026-01-02T00:00:00Z",
      memberCount: 1,
      mediaCount: 0,
    });
    expect(memory.cover).toBeUndefined();
  });

  test("never throws on null and fills defaults", () => {
    expect(toMemory(null)).toEqual({
      spaceId: "",
      type: "",
      name: "",
      from: "",
      until: "",
      memberCount: 0,
      mediaCount: 0,
      cover: undefined,
    });
  });
});

test.describe("toStats", () => {
  test("maps the 13 §3 sample exactly", () => {
    expect(
      toStats({
        encounters: { today: 12, week: 48, month: 160, total: 1024, uniquePeople: 380 },
        friends: 23,
        parties: { total: 15 },
        groups: { member: 6 },
        posts: { month: 210 },
      }),
    ).toEqual({
      encounters: { today: 12, week: 48, month: 160, total: 1024, uniquePeople: 380 },
      friends: 23,
      parties: { total: 15 },
      groups: { member: 6 },
      posts: { month: 210 },
    });
  });

  test("fills missing counts with zero", () => {
    expect(toStats({ encounters: { today: 3 } })).toEqual({
      encounters: { today: 3, week: 0, month: 0, total: 0, uniquePeople: 0 },
      friends: 0,
      parties: { total: 0 },
      groups: { member: 0 },
      posts: { month: 0 },
    });
  });

  test("never throws on {} and fills defaults", () => {
    expect(toStats({})).toEqual({
      encounters: { today: 0, week: 0, month: 0, total: 0, uniquePeople: 0 },
      friends: 0,
      parties: { total: 0 },
      groups: { member: 0 },
      posts: { month: 0 },
    });
  });
});

test.describe("toSettings", () => {
  test("maps every meSettingsResponse field", () => {
    expect(
      toSettings({
        encounterMode: "immediate",
        encounterDelaySec: 0,
        friendBeaconShare: false,
        partyAuto: false,
        locationPaused: true,
        warnDisplay: "hide",
        contentScanConsentVersion: 2,
        notify: { "friend.request": true },
      }),
    ).toEqual({
      encounterMode: "immediate",
      encounterDelaySec: 0,
      friendBeaconShare: false,
      partyAuto: false,
      locationPaused: true,
      warnDisplay: "hide",
      contentScanConsentVersion: 2,
      notify: { "friend.request": true },
    });
  });

  test("never throws on null and uses backend schema defaults", () => {
    expect(toSettings(null)).toEqual({
      encounterMode: "delayed",
      encounterDelaySec: 3600,
      friendBeaconShare: true,
      partyAuto: true,
      locationPaused: false,
      warnDisplay: "blur",
      notify: {},
    });
  });

  test("keeps a 0 delay and drops unknown enum values", () => {
    const settings = toSettings({
      encounterMode: "surprise",
      encounterDelaySec: 0,
      warnDisplay: "loud",
    } as never);
    expect(settings.encounterMode).toBe("delayed");
    expect(settings.encounterDelaySec).toBe(0);
    expect(settings.warnDisplay).toBe("blur");
  });
});

test.describe("fromSettings", () => {
  test("emits only the provided keys", () => {
    expect(fromSettings({ encounterMode: "private" })).toEqual({
      encounterMode: "private",
    });
  });

  test("never includes locationPaused or contentScanConsentVersion", () => {
    const patch = fromSettings({
      encounterMode: "delayed",
      encounterDelaySec: 3600,
      friendBeaconShare: false,
      partyAuto: true,
      locationPaused: true,
      warnDisplay: "blur",
      contentScanConsentVersion: 3,
      notify: { "encounter.daily": false },
    });
    expect(patch).toEqual({
      encounterMode: "delayed",
      encounterDelaySec: 3600,
      friendBeaconShare: false,
      partyAuto: true,
      warnDisplay: "blur",
      notify: { "encounter.daily": false },
    });
    expect("locationPaused" in patch).toBe(false);
    expect("contentScanConsentVersion" in patch).toBe(false);
  });

  test("returns an empty body for no changes", () => {
    expect(fromSettings({})).toEqual({});
  });
});

test.describe("toPin", () => {
  test("maps a pin and defaults an unknown reason to auto", () => {
    expect(
      toPin({
        uri: "at://did:plc:auth/jp.i32.chain.instance.pin/abc",
        subject: "at://did:plc:kenta/jp.i32.chain.instance.post/1",
        reason: "manual",
        pinnedBy: "did:plc:kenta",
        createdAt: "2026-10-17T02:30:00.000Z",
      }),
    ).toEqual({
      uri: "at://did:plc:auth/jp.i32.chain.instance.pin/abc",
      subject: "at://did:plc:kenta/jp.i32.chain.instance.post/1",
      reason: "manual",
      pinnedBy: "did:plc:kenta",
      createdAt: "2026-10-17T02:30:00.000Z",
    });
    expect(toPin(null)).toEqual({
      uri: "",
      subject: "",
      reason: "auto",
      pinnedBy: undefined,
      createdAt: "",
    });
  });
});
