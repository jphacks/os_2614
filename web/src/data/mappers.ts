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
  AttachmentDTO,
  FriendDTO,
  GroupDTO,
  MemoryDTO,
  PartyDTO,
  PostDTO,
  ProfileDTO,
  ReplyDTO,
  SettingsDTO,
  StatsDTO,
  UserDTO,
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
    id: dto.id,
    name: dto.name ?? "",
    avatarUrl: dto.avatar_url,
    tone: avatarToneFor(dto.id ?? ""),
  };
}

function toAttachment(dto: AttachmentDTO): Attachment {
  return {
    id: dto.id,
    kind: dto.kind ?? "image",
    url: dto.url,
    by: dto.by ? toUser(dto.by) : undefined,
  };
}

export function toProfile(dto: ProfileDTO): Profile {
  return {
    ...toUser(dto),
    bio: dto.bio ?? "",
    status: dto.status,
    bannerUrl: dto.banner_url,
    counts: {
      encounters: dto.counts?.encounters ?? 0,
      friends: dto.counts?.friends ?? 0,
      parties: dto.counts?.parties ?? 0,
    },
  };
}

export function toFriend(dto: FriendDTO): Friend {
  return {
    ...toUser(dto),
    together: dto.together ?? false,
    locationLabel: dto.location_label,
    locationVisibility: dto.location_visibility ?? "area",
    lastSeenAt: dto.last_seen_at,
  };
}

export function toGroup(dto: GroupDTO): Group {
  return {
    id: dto.id,
    name: dto.name ?? "",
    iconUrl: dto.icon_url,
    visibility: dto.visibility ?? "open",
    memberCount: dto.member_count ?? 0,
    areaLabel: dto.area_label ?? "",
    insideArea: dto.inside_area ?? true,
  };
}

export function toPost(dto: PostDTO): Post {
  return {
    id: dto.id,
    author: toUser(dto.author),
    body: dto.body ?? "",
    createdAt: dto.created_at,
    attachments: (dto.attachments ?? []).map(toAttachment),
    replyCount: dto.reply_count ?? 0,
    likeCount: dto.like_count ?? 0,
    likedByMe: dto.liked_by_me ?? false,
    pinned: dto.pinned ?? false,
  };
}

export function toReply(dto: ReplyDTO): Reply {
  return {
    id: dto.id,
    author: toUser(dto.author),
    body: dto.body ?? "",
    createdAt: dto.created_at,
    likeCount: dto.like_count ?? 0,
    likedByMe: dto.liked_by_me ?? false,
  };
}

export function toParty(dto: PartyDTO): Party {
  const photos = (dto.photos ?? []).map(toAttachment);
  return {
    id: dto.id,
    areaLabel: dto.area_label ?? "",
    stage: dto.stage ?? "provisional",
    startedAt: dto.started_at,
    fixed: dto.fixed ?? false,
    members: (dto.members ?? []).map((member) => ({
      ...toUser(member),
      viaFriend: member.via_friend ?? false,
    })),
    photos,
    photoCount: dto.photo_count ?? photos.length,
  };
}

export function toMemory(dto: MemoryDTO): Memory {
  const photos = (dto.photos ?? []).map(toAttachment);
  const members = (dto.members ?? []).map(toUser);
  return {
    id: dto.id,
    title: dto.title ?? "",
    startedAt: dto.started_at,
    endedAt: dto.ended_at,
    memberCount: dto.member_count ?? members.length,
    members,
    photos,
    photoCount: dto.photo_count ?? photos.length,
  };
}

export function toStats(dto: StatsDTO): Stats {
  return {
    range: dto.range ?? "month",
    encounters: {
      total: dto.encounters?.total ?? 0,
      delta: dto.encounters?.delta ?? 0,
      series: dto.encounters?.series ?? [],
    },
    friends: {
      total: dto.friends?.total ?? 0,
      delta: dto.friends?.delta ?? 0,
    },
    parties: {
      total: dto.parties?.total ?? 0,
      totalHours: dto.parties?.total_hours ?? 0,
    },
    peakHourLabel: dto.peak_hour_label ?? "",
    peakHourNote: dto.peak_hour_note ?? "",
    topArea: {
      label: dto.top_area?.label ?? "",
      ratio: dto.top_area?.ratio ?? 0,
    },
    activity: dto.activity ?? [],
    messagesThisMonth: dto.messages_this_month ?? 0,
  };
}

export function toSettings(dto: SettingsDTO): Settings {
  let revealDelay: Settings["revealDelay"];
  if (dto.reveal_delay_hours === undefined) {
    revealDelay = { hours: 3 };
  } else if (dto.reveal_delay_hours === null) {
    revealDelay = null;
  } else {
    revealDelay = { hours: dto.reveal_delay_hours };
  }

  return {
    revealDelay,
    allowFriendRequests: dto.allow_friend_requests ?? true,
    locationVisibility: dto.location_visibility ?? "area",
    restrictToAreas: dto.restrict_to_areas ?? false,
    sharedAreas: dto.shared_areas ?? [],
  };
}

export function fromSettings(settings: Settings): SettingsDTO {
  return {
    reveal_delay_hours: settings.revealDelay?.hours ?? null,
    allow_friend_requests: settings.allowFriendRequests,
    location_visibility: settings.locationVisibility,
    restrict_to_areas: settings.restrictToAreas,
    shared_areas: settings.sharedAreas,
  };
}
