"use client";

// リアルタイムイベントの振り分け (specification/backend/10-instance.md §7.3)。
//
// ws.ts (todo 27) は 1 本の接続・購読・再接続を持つ。ここはその上で次を持つ:
// - `event` フレームを種別ごとに購読者へ配る (未知の種別は無視する)
// - 画面が Space を購読/解除する `subscribe(spaceId)` API
// - React から 1 種類のイベントを購読する `useRealtimeEvent` hook
// - 再接続後に REST で取り直すための seam (`onResync` / `notifyReconnected`)
//
// 配信は at-most-once なので、取りこぼしは再接続後の REST 再取得で補う (10 §7.3)。
// 接続・購読のライフサイクル (1 本だけ張る provider) は todo 29 が持つ。
// ここはフレームの解釈と購読者の管理だけを行う。
//
// data の形は backend が publish する値 (service の *Item) に合わせ、src/data/dto.ts の
// 型で表す。マッパーを通した domain 型ではなく、ワイヤ上の形をそのまま購読者へ渡す。
// 表示用の変換は購読側 (画面) が src/data/mappers.ts で行う。

import { createContext, useContext, useEffect, useRef } from "react";

import type {
  Encounter as EncounterDTO,
  InstancePost,
  Media as MediaDTO,
  Notification as NotificationDTO,
  PartyPhase,
  Pin as PinDTO,
  User as UserDTO,
} from "@/data/dto";
import type { EventFrame, RealtimeEvents, RealtimeState } from "./ws";

/** `party.updated` の data。在席メンバーは DID の一覧 (backend service.publishPartyUpdated)。 */
export interface PartyUpdatedData {
  phase: PartyPhase;
  locked: boolean;
  present: string[];
}

/** 既知のイベント名と、その data の形 (10 §7.3 の表)。 */
export interface RealtimeEventDataMap {
  "post.created": InstancePost;
  "post.deleted": { uri: string };
  "media.created": MediaDTO;
  "pin.created": PinDTO;
  "pin.deleted": { uri: string };
  "member.joined": { did: string };
  "member.left": { did: string };
  "space.archived": undefined;
  "party.updated": PartyUpdatedData;
  "encounter.revealed": EncounterDTO;
  "friend.request": UserDTO;
  "friend.accepted": UserDTO;
  "friend.changed": UserDTO;
  "notification": NotificationDTO;
}

export type RealtimeEventName = keyof RealtimeEventDataMap;

/** 全ての既知イベント名。テストの網羅と、未知判定の元になる。 */
export const REALTIME_EVENT_NAMES = [
  "post.created",
  "post.deleted",
  "media.created",
  "pin.created",
  "pin.deleted",
  "member.joined",
  "member.left",
  "space.archived",
  "party.updated",
  "encounter.revealed",
  "friend.request",
  "friend.accepted",
  "friend.changed",
  "notification",
] as const satisfies readonly RealtimeEventName[];

// REALTIME_EVENT_NAMES が RealtimeEventName を過不足なく覆っていることを型で固定する。
// どれか足す/消すとこの代入が壊れる。
type MissingEventName = Exclude<RealtimeEventName, (typeof REALTIME_EVENT_NAMES)[number]>;
const _allEventsCovered: MissingEventName extends never ? true : never = true;
void _allEventsCovered;

const KNOWN_EVENTS: ReadonlySet<string> = new Set(REALTIME_EVENT_NAMES);

/** 10 §7.3 の表にあるイベント名か。未知は false。 */
export function isKnownEvent(name: string): name is RealtimeEventName {
  return KNOWN_EVENTS.has(name);
}

export type RealtimeEventHandler<K extends RealtimeEventName> = (
  data: RealtimeEventDataMap[K],
  frame: EventFrame,
) => void;

/**
 * RealtimeEventRouter が必要とする ws.ts の公開面。
 * RealtimeClient はこれを構造的に満たす (テストでは軽いスタブを渡せる)。
 */
export interface RealtimeTransport {
  on<K extends keyof RealtimeEvents>(
    event: K,
    handler: (payload: RealtimeEvents[K]) => void,
  ): () => void;
  subscribe(space: string): string;
  unsubscribe(space: string): void;
  readonly subscribedSpaces: string[];
}

/**
 * `event` フレームを種別ごとの購読者へ配り、Space 購読と再接続時の REST 再取得を束ねる。
 * React には依存しない (context 経由で hook に渡す)。
 */
export class RealtimeEventRouter {
  private readonly transport: RealtimeTransport;
  private readonly handlers = new Map<
    RealtimeEventName,
    Set<(data: unknown, frame: EventFrame) => void>
  >();
  private readonly resyncHandlers = new Set<() => void>();
  /** 一度でも再接続に入ったか。最初の ready では resync しないため。 */
  private sawDisconnect = false;
  private detachers: Array<() => void> = [];

  constructor(transport: RealtimeTransport) {
    this.transport = transport;
  }

  /** transport の event/state を購読して配り始める。戻り値を呼ぶと解除する。 */
  attach(): () => void {
    this.detach();
    this.detachers = [
      this.transport.on("event", this.dispatch),
      this.transport.on("state", this.onState),
    ];
    return () => this.detach();
  }

  /** event/state の購読を外す。resync / 種別ごとの購読者は残す。 */
  detach(): void {
    for (const off of this.detachers) {
      off();
    }
    this.detachers = [];
  }

  /**
   * `event` フレームを購読者へ配る。未知のイベント名は無視して false を返す
   * (例外にしない)。既知なら true。
   */
  dispatch = (frame: EventFrame): boolean => {
    if (!isKnownEvent(frame.event)) {
      return false;
    }
    const set = this.handlers.get(frame.event);
    if (set !== undefined) {
      for (const handler of set) {
        handler(frame.data, frame);
      }
    }
    return true;
  };

  /** 種別を指定して購読する。戻り値を呼ぶと解除する。 */
  on<K extends RealtimeEventName>(type: K, handler: RealtimeEventHandler<K>): () => void {
    let set = this.handlers.get(type);
    if (set === undefined) {
      set = new Set();
      this.handlers.set(type, set);
    }
    const wrapped = (data: unknown, frame: EventFrame) =>
      handler(data as RealtimeEventDataMap[K], frame);
    set.add(wrapped);
    return () => {
      set.delete(wrapped);
    };
  }

  /** Space を購読する。再接続後の再送は ws.ts が面倒を見る (10 §7.2)。 */
  subscribe(spaceId: string): string {
    return this.transport.subscribe(spaceId);
  }

  unsubscribe(spaceId: string): void {
    this.transport.unsubscribe(spaceId);
  }

  get subscribedSpaces(): string[] {
    return this.transport.subscribedSpaces;
  }

  /**
   * 再接続後の REST 再取得ハンドラを登録する (10 §7.3: at-most-once の取りこぼし補填)。
   * 対象 REST は呼び出し側 (provider / 画面, todo 29) が知っている。
   */
  onResync(handler: () => void): () => void {
    this.resyncHandlers.add(handler);
    return () => {
      this.resyncHandlers.delete(handler);
    };
  }

  /** 今すぐ再取得ハンドラを走らせる。provider が接続復帰時に直接呼んでもよい。 */
  notifyReconnected(): void {
    for (const handler of this.resyncHandlers) {
      handler();
    }
  }

  private onState = (state: RealtimeState): void => {
    if (state === "reconnecting") {
      this.sawDisconnect = true;
      return;
    }
    if (state === "ready" && this.sawDisconnect) {
      this.sawDisconnect = false;
      this.notifyReconnected();
    }
  };
}

/**
 * 現在有効な router。todo 29 の provider が `RealtimeEventsContext.Provider` で配る。
 * 未設置のときは null で、hook は何もしない。
 */
export const RealtimeEventsContext = createContext<RealtimeEventRouter | null>(null);

/** 画面から `subscribe(spaceId)` するための router 取得。無ければ null。 */
export function useRealtimeRouter(): RealtimeEventRouter | null {
  return useContext(RealtimeEventsContext);
}

/**
 * 1 種類のリアルタイムイベントを購読する。router が無い (provider 未設置) ときは
 * 何もしない。handler は常に最新を ref 経由で呼ぶので、再購読は起きない。
 */
export function useRealtimeEvent<K extends RealtimeEventName>(
  type: K,
  handler: RealtimeEventHandler<K>,
): void {
  const router = useContext(RealtimeEventsContext);
  const latest = useRef(handler);
  useEffect(() => {
    latest.current = handler;
  });
  useEffect(() => {
    if (router === null) {
      return;
    }
    return router.on(type, (data, frame) => latest.current(data, frame));
  }, [router, type]);
}
