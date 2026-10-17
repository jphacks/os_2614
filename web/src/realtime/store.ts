"use client";

// リアルタイム派生のクライアントキャッシュ (todo 28 / specification/backend/10 §7.3)。
//
// ws.ts (todo 27) が 1 本の接続を持ち、events.ts が `event` フレームを種別ごとに
// 配る。ここはその上で「イベントを状態へ反映した結果」を保持する:
// - RealtimeStore は 10 §7.3 の全イベント種別を 1 つのイミュータブルな snapshot に畳む。
//   未知のイベント名は無視して例外にしない。
// - subscribe / getSnapshot を持つので React からは useSyncExternalStore で読める
//   (useRealtimeCache)。effect 本体で setState しないので react-hooks/set-state-in-effect に触れない。
// - attachRealtimeStore が router の全既知イベントを store へ流す。provider が接続の
//   開始/停止に合わせて attach/detach し、サインアウトで reset する (残状態を持ち越さない)。
//
// 配信は at-most-once なので、取りこぼしは再接続後の REST 再取得で補う (10 §7.3)。
// 画面はこのキャッシュを「表示中の一覧へ重ねる」ために読み、再接続時は REST を取り直す。
// 値はワイヤ上の形 (src/data/dto.ts) のまま持ち、表示用の変換は画面が mappers で行う。

import { useSyncExternalStore } from "react";

import type {
  Encounter as EncounterDTO,
  InstancePost,
  Media as MediaDTO,
  Notification as NotificationDTO,
  Pin as PinDTO,
  User as UserDTO,
} from "@/data/dto";
import {
  isKnownEvent,
  REALTIME_EVENT_NAMES,
  type PartyUpdatedData,
  type RealtimeEventDataMap,
  type RealtimeEventName,
  type RealtimeEventRouter,
} from "./events";
import type { EventFrame } from "./ws";

/**
 * 1 つの接続が受け取るフレーム数は無制限なので、配列は上限を設けて古いものを落とす。
 * 溢れた分は再接続後の REST 再取得で戻る (10 §7.3)。
 */
const MAX_LIST = 200;

/** Space 購読イベント (post.* / media.* / pin.* / member.* / space.archived) の差分。 */
export interface RealtimeSpaceState {
  /** `post.created` で届いた投稿 (新しい順)。 */
  posts: InstancePost[];
  /** `post.deleted` で消えた投稿の uri。 */
  deletedPosts: string[];
  /** `media.created` で届いたメディア (新しい順)。 */
  media: MediaDTO[];
  /** `pin.created` で届いた Pin (新しい順)。 */
  pins: PinDTO[];
  /** `pin.deleted` で消えた Pin の uri。 */
  deletedPins: string[];
  /** `member.joined` で加わった DID。 */
  joined: string[];
  /** `member.left` で抜けた DID。 */
  left: string[];
  /** `space.archived` を受けたか。 */
  archived: boolean;
}

export type FriendEventKind = "request" | "accepted" | "changed";

/** `friend.*` (ユーザー購読) の 1 件。payload は相手の UserSummary。 */
export interface RealtimeFriendEvent {
  kind: FriendEventKind;
  user: UserDTO;
}

/**
 * リアルタイム由来の状態。変更のたびに新しいオブジェクトへ差し替える
 * (useSyncExternalStore が参照の同一性で再描画を判定できる)。
 */
export interface RealtimeCache {
  /** 変更のたびに増える。snapshot の同一性判定とテストの補助に使う。 */
  readonly revision: number;
  /** `notification` で届いた通知 (新しい順)。 */
  readonly notifications: NotificationDTO[];
  /** `encounter.revealed` で届いたすれ違い (新しい順)。 */
  readonly encounters: EncounterDTO[];
  /** `friend.request` / `friend.accepted` / `friend.changed` (新しい順)。 */
  readonly friendEvents: RealtimeFriendEvent[];
  /** `party.updated` の最新値。未受信は null。 */
  readonly party: PartyUpdatedData | null;
  /** Space ごとの差分。キーは spaceId (無ければ空文字)。 */
  readonly spaces: Readonly<Record<string, RealtimeSpaceState>>;
}

function emptySpace(): RealtimeSpaceState {
  return {
    posts: [],
    deletedPosts: [],
    media: [],
    pins: [],
    deletedPins: [],
    joined: [],
    left: [],
    archived: false,
  };
}

function emptyCache(): RealtimeCache {
  return {
    revision: 0,
    notifications: [],
    encounters: [],
    friendEvents: [],
    party: null,
    spaces: {},
  };
}

/** key が同じ既存要素を除いて先頭へ足す (重複を残さない)。 */
function prepend<T>(
  list: readonly T[],
  item: T,
  keyOf: (value: T) => string,
  max = MAX_LIST,
): T[] {
  const key = keyOf(item);
  const rest = list.filter((value) => keyOf(value) !== key);
  return [item, ...rest].slice(0, max);
}

/** 文字列を重複なく先頭へ足す。 */
function addUnique(list: readonly string[], value: string, max = MAX_LIST): string[] {
  return [value, ...list.filter((item) => item !== value)].slice(0, max);
}

/** frame の space、無ければ data.spaceId。Space 購読イベントのキー。 */
function spaceKeyOf(frame: EventFrame | undefined, data: unknown): string {
  if (frame?.space) {
    return frame.space;
  }
  const spaceId = (data as { spaceId?: unknown } | null | undefined)?.spaceId;
  return typeof spaceId === "string" ? spaceId : "";
}

/**
 * リアルタイム由来の状態を持つ小さな store。React に依存しないので、
 * router と組にしてブラウザ無しでテストできる。
 */
export class RealtimeStore {
  private state: RealtimeCache = emptyCache();
  private readonly listeners = new Set<() => void>();

  /** useSyncExternalStore の subscribe。 */
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  /** useSyncExternalStore の getSnapshot / getServerSnapshot。 */
  getSnapshot = (): RealtimeCache => this.state;

  /** サインアウトなどで捨てる。空のキャッシュに戻す。 */
  reset(): void {
    this.commit(emptyCache());
  }

  /**
   * 1 つのイベントをキャッシュへ反映する。未知のイベント名は無視して false を返す
   * (例外にしない)。既知なら true。既知でも状態が変わらない場合は通知しない。
   */
  apply(name: string, data: unknown, frame?: EventFrame): boolean {
    if (!isKnownEvent(name)) {
      return false;
    }
    this.commit(this.reduce(this.state, name, data as RealtimeEventDataMap[typeof name], frame));
    return true;
  }

  private reduce(
    state: RealtimeCache,
    name: RealtimeEventName,
    data: RealtimeEventDataMap[RealtimeEventName],
    frame: EventFrame | undefined,
  ): RealtimeCache {
    const revision = state.revision + 1;
    switch (name) {
      case "notification":
        return {
          ...state,
          revision,
          notifications: prepend(
            state.notifications,
            data as NotificationDTO,
            (item) => item.id,
          ),
        };
      case "encounter.revealed":
        return {
          ...state,
          revision,
          encounters: prepend(
            state.encounters,
            data as EncounterDTO,
            (item) => item.id,
          ),
        };
      case "friend.request":
      case "friend.accepted":
      case "friend.changed": {
        const kind: FriendEventKind =
          name === "friend.request"
            ? "request"
            : name === "friend.accepted"
              ? "accepted"
              : "changed";
        const event: RealtimeFriendEvent = { kind, user: data as UserDTO };
        return {
          ...state,
          revision,
          friendEvents: prepend(
            state.friendEvents,
            event,
            (item) => `${item.kind}:${item.user.did}`,
          ),
        };
      }
      case "party.updated":
        return { ...state, revision, party: data as PartyUpdatedData };
      case "post.created":
      case "post.deleted":
      case "media.created":
      case "pin.created":
      case "pin.deleted":
      case "member.joined":
      case "member.left":
      case "space.archived": {
        const key = spaceKeyOf(frame, data);
        const current = state.spaces[key] ?? emptySpace();
        const next = reduceSpace(current, name, data);
        return { ...state, revision, spaces: { ...state.spaces, [key]: next } };
      }
      default:
        return state;
    }
  }

  private commit(next: RealtimeCache): void {
    if (next === this.state) {
      return;
    }
    this.state = next;
    for (const listener of this.listeners) {
      listener();
    }
  }
}

/** Space 購読イベントを 1 つの Space 差分へ畳む。 */
function reduceSpace(
  state: RealtimeSpaceState,
  name: RealtimeEventName,
  data: unknown,
): RealtimeSpaceState {
  switch (name) {
    case "post.created": {
      const post = data as InstancePost;
      return { ...state, posts: prepend(state.posts, post, (item) => item.uri) };
    }
    case "post.deleted": {
      const uri = (data as { uri: string }).uri;
      return {
        ...state,
        posts: state.posts.filter((post) => post.uri !== uri),
        deletedPosts: addUnique(state.deletedPosts, uri),
      };
    }
    case "media.created": {
      const media = data as MediaDTO;
      return { ...state, media: prepend(state.media, media, (item) => item.id) };
    }
    case "pin.created": {
      const pin = data as PinDTO;
      return { ...state, pins: prepend(state.pins, pin, (item) => item.uri) };
    }
    case "pin.deleted": {
      const uri = (data as { uri: string }).uri;
      return {
        ...state,
        pins: state.pins.filter((pin) => pin.uri !== uri),
        deletedPins: addUnique(state.deletedPins, uri),
      };
    }
    case "member.joined": {
      const did = (data as { did: string }).did;
      return {
        ...state,
        joined: addUnique(state.joined, did),
        left: state.left.filter((item) => item !== did),
      };
    }
    case "member.left": {
      const did = (data as { did: string }).did;
      return {
        ...state,
        left: addUnique(state.left, did),
        joined: state.joined.filter((item) => item !== did),
      };
    }
    case "space.archived":
      return { ...state, archived: true };
    default:
      return state;
  }
}

/**
 * router の全既知イベントを store へ流す。戻り値を呼ぶと解除する。
 * provider が接続の開始/停止に合わせて attach / detach する。
 */
export function attachRealtimeStore(
  router: RealtimeEventRouter,
  store: RealtimeStore = realtimeStore,
): () => void {
  const offs = REALTIME_EVENT_NAMES.map((name) =>
    router.on(name, (data, frame) => {
      store.apply(name, data, frame);
    }),
  );
  return () => {
    for (const off of offs) {
      off();
    }
  };
}

/** アプリ全体で 1 つ。provider が接続する間だけ更新され、サインアウトで reset する。 */
export const realtimeStore = new RealtimeStore();

/**
 * キャッシュ全体を購読する。provider が無くても (未ログイン) 常に snapshot を返し、
 * そのときは空のキャッシュになる。
 */
export function useRealtimeCache(): RealtimeCache {
  return useSyncExternalStore(
    realtimeStore.subscribe,
    realtimeStore.getSnapshot,
    realtimeStore.getSnapshot,
  );
}
