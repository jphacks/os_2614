// DTO → ドメイン変換。純関数で例外を投げず、欠損は既定値で埋める。
// UI はこのファイルを経由した値だけを見る (DTO を UI に持ち込まない)。

import type {
  Attachment,
  AvatarTone,
  Friend,
  Group,
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

/** ユーザー ID の文字コード和 mod 8 でアバター背景色を決める */
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
    id: dto.did,
    name: dto.displayName || dto.handle,
    avatarUrl: dto.avatarUrl,
    tone: avatarToneFor(dto.did),
  };
}

function toAttachment(dto: MediaDTO): Attachment {
  return {
    id: dto.id,
    kind: "image",
    url: dto.url,
    by: undefined,
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
    together: false,
    locationLabel: undefined,
    locationVisibility:
      dto.precision === "direction"
        ? "direction"
        : dto.precision === "off"
          ? "hidden"
          : "area",
    lastSeenAt: undefined,
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
    attachments: (dto.media ?? []).map(toAttachment),
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
  const photos: Attachment[] = [];
  return {
    id: dto.id,
    areaLabel: dto.name,
    stage: dto.phase === "confirmed" ? "active" : "provisional",
    startedAt: dto.tentativeAt,
    fixed: dto.locked,
    members: dto.members.map((member) => ({
      id: member.did,
      name: member.displayName || member.handle || "",
      avatarUrl: member.avatarUrl,
      tone: avatarToneFor(member.did),
      viaFriend: false,
    })),
    photos,
    photoCount: photos.length,
  };
}

export function toMemory(dto: MemoryDTO): Memory {
  return {
    id: dto.spaceId,
    title: dto.name,
    startedAt: dto.from,
    endedAt: dto.until,
    memberCount: dto.memberCount,
    members: [],
    photos: [],
    photoCount: dto.mediaCount,
  };
}

export function toStats(dto: StatsDTO): Stats {
  return {
    range: "month",
    encounters: {
      total: dto.encounters.total,
      delta: 0,
      series: [],
    },
    friends: {
      total: dto.friends,
      delta: 0,
    },
    parties: {
      total: dto.parties.total,
      totalHours: 0,
    },
    peakHourLabel: "",
    peakHourNote: "",
    topArea: {
      label: "",
      ratio: 0,
    },
    activity: [],
    messagesThisMonth: dto.posts.month,
  };
}

export function toSettings(dto: SettingsDTO): Settings {
  let revealDelay: Settings["revealDelay"];
  if (dto.encounterMode === "private") {
    revealDelay = null;
  } else if (dto.encounterMode === "immediate") {
    revealDelay = { hours: 0 };
  } else {
    revealDelay = { hours: Math.round(dto.encounterDelaySec / 3600) };
  }

  return {
    revealDelay,
    allowFriendRequests: true,
    locationVisibility: "area",
    restrictToAreas: false,
    sharedAreas: [],
  };
}

export function fromSettings(settings: Settings): SettingsDTO {
  return {
    encounterMode:
      settings.revealDelay === null
        ? "private"
        : settings.revealDelay.hours === 0
          ? "immediate"
          : "delayed",
    encounterDelaySec:
      settings.revealDelay && settings.revealDelay.hours > 0
        ? settings.revealDelay.hours * 3600
        : 3600,
    friendBeaconShare: false,
    partyAuto: false,
    locationPaused: false,
    warnDisplay: "show",
    notify: {},
  };
}
