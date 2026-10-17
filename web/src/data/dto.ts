// backend (Go/Fiber) の JSON レスポンスを想定した仮 DTO。
// 命名は snake_case、時刻は ISO 8601 文字列、任意項目は `?:`。
// backend が確定したらこのファイルと mappers.ts だけを直す。

export type ID = string;
export type ISODateString = string;

export type UserDTO = {
  id: ID;
  name: string;
  avatar_url?: string;
};

export type ProfileDTO = UserDTO & {
  bio?: string;
  status?: string;
  banner_url?: string;
  counts?: {
    encounters?: number;
    friends?: number;
    parties?: number;
  };
};

export type LocationVisibilityDTO = "area" | "direction" | "hidden";

export type FriendDTO = UserDTO & {
  together?: boolean;
  location_label?: string;
  location_visibility?: LocationVisibilityDTO;
  last_seen_at?: ISODateString;
};

export type GroupVisibilityDTO = "open" | "closed";

export type GroupDTO = {
  id: ID;
  name: string;
  icon_url?: string;
  visibility?: GroupVisibilityDTO;
  member_count?: number;
  area_label?: string;
  inside_area?: boolean;
};

export type AttachmentDTO = {
  id: ID;
  kind: "image";
  url: string;
  by?: UserDTO;
};

export type PostDTO = {
  id: ID;
  author: UserDTO;
  body?: string;
  created_at: ISODateString;
  attachments?: AttachmentDTO[];
  reply_count?: number;
  like_count?: number;
  liked_by_me?: boolean;
  pinned?: boolean;
};

export type ReplyDTO = {
  id: ID;
  author: UserDTO;
  body?: string;
  created_at: ISODateString;
  like_count?: number;
  liked_by_me?: boolean;
};

export type PartyStageDTO = "provisional" | "active";

export type PartyMemberDTO = UserDTO & {
  via_friend?: boolean;
};

export type PartyDTO = {
  id: ID;
  area_label?: string;
  stage?: PartyStageDTO;
  started_at: ISODateString;
  fixed?: boolean;
  members?: PartyMemberDTO[];
  photos?: AttachmentDTO[];
  photo_count?: number;
};

export type MemoryDTO = {
  id: ID;
  title?: string;
  started_at: ISODateString;
  ended_at: ISODateString;
  member_count?: number;
  members?: UserDTO[];
  photos?: AttachmentDTO[];
  photo_count?: number;
};

export type StatsRangeDTO = "week" | "month" | "year" | "all";

export type StatsDTO = {
  range?: StatsRangeDTO;
  encounters?: {
    total?: number;
    delta?: number;
    series?: { label: string; value: number }[];
  };
  friends?: { total?: number; delta?: number };
  parties?: { total?: number; total_hours?: number };
  peak_hour_label?: string;
  peak_hour_note?: string;
  top_area?: { label?: string; ratio?: number };
  activity?: number[][];
  messages_this_month?: number;
};

export type SettingsDTO = {
  reveal_delay_hours?: number | null;
  allow_friend_requests?: boolean;
  location_visibility?: LocationVisibilityDTO;
  restrict_to_areas?: boolean;
  shared_areas?: string[];
};

export type PostTargetDTO =
  | { kind: "group"; id: ID; name: string; member_count?: number }
  | { kind: "party"; id: ID; name: string };
