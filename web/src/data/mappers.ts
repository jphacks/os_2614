// DTO → ドメイン変換。純関数で例外を投げず、欠損は既定値で埋める。
// UI はこのファイルを経由した値だけを見る (DTO を UI に持ち込まない)。

import type {
  AvatarTone,
  Friend,
  Group,
  Media,
  Memory,
  Party,
  Post,
  Profile,
  Reply,
  Settings,
  Stats,
  User,
} from "@/domain/types";
import type {
  Friend as FriendDTO,
  Group as GroupDTO,
  InstancePost as PostDTO,
  Media as MediaDTO,
  Memory as MemoryDTO,
  Party as PartyDTO,
  Profile as ProfileDTO,
  Settings as SettingsDTO,
  Stats as StatsDTO,
  User as UserDTO,
} from "./dto";

const TONES: AvatarTone[] = [
  "peach",
  "blue",
  "green",
  "lavender",
  "mint",
  "yellow",
  "periwinkle",
  "sand",
];

/** ユーザー DID の文字コード和 mod 8 でアバター背景色を決める */
export function avatarToneFor(id: string): AvatarTone {
  let sum = 0;
  for (const char of id) {
    sum += char.codePointAt(0) ?? 0;
  }
  const index = ((sum % TONES.length) + TONES.length) % TONES.length;
  return TONES[index];
}

export function toUser(dto: UserDTO): User {
  return {
    did: dto.did,
    handle: dto.handle,
    name: dto.displayName || dto.handle,
    avatarUrl: dto.avatarUrl,
    tone: avatarToneFor(dto.did),
  };
}

function toMedia(dto: MediaDTO): Media {
  return {
    id: dto.id,
    alt: dto.alt,
    url: dto.url,
    thumbUrl: dto.thumbUrl,
    width: dto.width,
    height: dto.height,
  };
}

export function toProfile(dto: ProfileDTO): Profile {
  return {
    ...toUser(dto),
    bio: dto.description ?? "",
    status: dto.status,
    bannerUrl: dto.bannerUrl,
    counts: {
      encounters: 0,
      friends: 0,
      parties: 0,
    },
  };
}

export function toFriend(dto: FriendDTO): Friend {
  return {
    ...toUser(dto),
    since: dto.since,
    precision: dto.precision,
  };
}

export function toGroup(dto: GroupDTO): Group {
  return {
    id: dto.id,
    name: dto.name,
    iconUrl: undefined,
    visibility: dto.visibility,
    memberCount: dto.memberCount,
    areaLabel: "",
    insideArea: dto.isMember,
  };
}

export function toPost(dto: PostDTO): Post {
  return {
    id: dto.uri,
    author: toUser(dto.author ?? { did: "", handle: "" }),
    body: dto.text ?? "",
    createdAt: dto.createdAt,
    attachments: (dto.media ?? []).map(toMedia),
    replyCount: 0,
    likeCount: 0,
    likedByMe: false,
    pinned: false,
  };
}

export function toReply(dto: PostDTO): Reply {
  return {
    id: dto.uri,
    author: toUser(dto.author ?? { did: "", handle: "" }),
    body: dto.text ?? "",
    createdAt: dto.createdAt,
    likeCount: 0,
    likedByMe: false,
  };
}

export function toParty(dto: PartyDTO): Party {
  return {
    id: dto.id,
    name: dto.name,
    phase: dto.phase,
    locked: dto.locked,
    tentativeAt: dto.tentativeAt,
    confirmedAt: dto.confirmedAt,
    members: dto.members,
  };
}

export function toMemory(dto: MemoryDTO): Memory {
  return {
    spaceId: dto.spaceId,
    type: dto.type,
    name: dto.name,
    from: dto.from,
    until: dto.until,
    memberCount: dto.memberCount,
    mediaCount: dto.mediaCount,
    cover: dto.cover,
  };
}

export function toStats(dto: StatsDTO): Stats {
  return {
    encounters: dto.encounters,
    friends: dto.friends,
    parties: dto.parties,
    groups: dto.groups,
    posts: dto.posts,
  };
}

export function toSettings(dto: SettingsDTO): Settings {
  return {
    encounterMode: dto.encounterMode,
    encounterDelaySec: dto.encounterDelaySec,
    friendBeaconShare: dto.friendBeaconShare,
    partyAuto: dto.partyAuto,
    locationPaused: dto.locationPaused,
    warnDisplay: dto.warnDisplay,
    contentScanConsentVersion: dto.contentScanConsentVersion,
    notify: dto.notify,
  };
}

export function fromSettings(settings: Settings): SettingsDTO {
  return {
    encounterMode: settings.encounterMode,
    encounterDelaySec: settings.encounterDelaySec,
    friendBeaconShare: settings.friendBeaconShare,
    partyAuto: settings.partyAuto,
    locationPaused: settings.locationPaused,
    warnDisplay: settings.warnDisplay,
    contentScanConsentVersion: settings.contentScanConsentVersion,
    notify: settings.notify,
  };
}
