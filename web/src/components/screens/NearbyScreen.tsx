"use client";

import { useRouter } from "next/navigation";
import { useCallback, useMemo, useState } from "react";
import type {
  Encounter,
  Friend,
  FriendLocation,
  GroupNearby,
  Party,
  User,
} from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import {
  NearbyBubble,
  buildNearbyModel,
  layoutNearby,
  type GroupArea,
  type LatLon,
  type NearbyBubbleModel,
} from "@/components/feature/nearby";
import { AppShell } from "@/components/layout/AppShell";
import { useSplashDone } from "@/components/layout/StartupSplash";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { BubbleCanvas } from "@/components/ui/BubbleCanvas";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Notice } from "@/components/ui/Notice";
import { PressableLink } from "@/components/ui/Pressable";
import styles from "./NearbyScreen.module.css";

export type NearbyScreenProps = {
  /** 自分 (GET /me)。取れるまでは null で、中央は仮の泡にする。 */
  me: User | null;
  /** 端末の現在地。取れない間は null (座標から解く相手は「遠く」に回る)。 */
  myLocation: LatLon | null;
  /** 端末の向き (度、北 0・時計回り)。センサーが無ければ null で北を上にする。 */
  heading: number | null;
  friends: Friend[];
  /** GET /friends/locations (07 §4.2) を did で引けるようにしたもの。 */
  friendLocations: ReadonlyMap<string, FriendLocation>;
  /** GET /encounters (06 §6)。新しい順。 */
  encounters: Encounter[];
  /** GET /party/current。所属していなければ null。 */
  party: Party | null;
  /** GET /groups/nearby (09 §5) の取得状態。 */
  groups: ResourceState<GroupNearby>;
  /** 中心が分かっているグループ (GET /groups/{id}、メンバーのときだけ返る)。 */
  groupAreas: ReadonlyMap<string, GroupArea>;
  /** POST /groups/{id}/join (09 §5)。成功しなければ reject する。 */
  onJoin: (id: string) => Promise<void>;
  /** POST /groups/{id}/leave (09 §5)。成功しなければ reject する。 */
  onLeave: (id: string) => Promise<void>;
  /** error 状態の再取得。 */
  onRetry?: () => void;
  /** 403 location_stale のときの位置要求 (権限要求 → 再取得)。 */
  onEnableLocation?: () => void;
};

type Item = {
  id: string;
  x: number;
  y: number;
  size: number;
  fixed: boolean;
  /** 現れるアニメーションの遅れ (ms)。近いものから順に出す */
  delay: number;
  bubble: NearbyBubbleModel;
};

/** 泡が 1 つずつ現れる間隔 (ms) と、その上限。 */
const APPEAR_STEP_MS = 40;
const APPEAR_MAX_MS = 600;

const PLACEHOLDER_ME: User = { did: "", handle: "", name: "", tone: "blue" };

/** 泡をタップしたときの行き先。 */
function hrefOf(bubble: NearbyBubbleModel): string | null {
  switch (bubble.kind) {
    case "plus":
      return "/group/new/";
    case "party":
      return "/party/";
    case "group":
      return `/group/?id=${encodeURIComponent(bubble.groupId)}`;
    case "person":
      return bubble.user.did ? `/users/?did=${encodeURIComponent(bubble.user.did)}` : null;
    case "me":
      return "/profile/";
  }
}

export function NearbyScreen({
  me,
  myLocation,
  heading,
  friends,
  friendLocations,
  encounters,
  party,
  groups,
  groupAreas,
  onJoin,
  onLeave,
  onRetry,
  onEnableLocation,
}: NearbyScreenProps) {
  const router = useRouter();
  const splashDone = useSplashDone();
  const [focusedId, setFocusedId] = useState<string | null>(null);
  const [pending, setPending] = useState<Record<string, "join" | "leave">>({});
  const [actionError, setActionError] = useState<string | null>(null);

  const groupData = groups.status === "ok" ? groups.data : null;

  const model = useMemo(
    () =>
      buildNearbyModel({
        me: me ?? PLACEHOLDER_ME,
        myLocation,
        friends,
        friendLocations,
        encounters,
        party,
        groups: groupData,
        groupAreas,
      }),
    [me, myLocation, friends, friendLocations, encounters, party, groupData, groupAreas],
  );

  // 重なりの解消は重いので、データが変わったときだけやり直す。向きは毎回回すだけ
  const layout = useMemo(
    () =>
      layoutNearby({
        me: model.me,
        plus: model.plus,
        ring: model.ring,
        nodes: model.nodes.map(({ bubble, placement }) => ({
          id: bubble.id,
          size: bubble.size,
          placement,
        })),
      }),
    [model],
  );

  const bubbles = useMemo(() => {
    const map = new Map<string, NearbyBubbleModel>();
    for (const bubble of [model.me, model.plus, ...model.ring]) map.set(bubble.id, bubble);
    for (const { bubble } of model.nodes) map.set(bubble.id, bubble);
    return map;
  }, [model]);

  // 向きでは座標を変えず、BubbleCanvas の rotation で自分の周りをなめらかに回す
  const items = useMemo(() => {
    const list: Item[] = [];
    const nodes = [
      // 回さないのは中心の自分だけ。+ やパーティーも周りと一緒に自分の周りを回る
      ...layout.inner.map((node) => ({ node, fixed: node.id === model.me.id })),
      ...layout.outer.map((node) => ({ node, fixed: false })),
    ];
    nodes.forEach(({ node, fixed }, index) => {
      const bubble = bubbles.get(node.id);
      if (!bubble) return;
      const delay = Math.min(APPEAR_MAX_MS, index * APPEAR_STEP_MS);
      list.push({ ...node, fixed, delay, bubble });
    });
    return list;
  }, [layout, bubbles, model.me.id]);

  const run = useCallback(
    async (id: string, kind: "join" | "leave") => {
      setActionError(null);
      setPending((prev) => ({ ...prev, [id]: kind }));
      try {
        if (kind === "join") {
          await onJoin(id);
        } else {
          await onLeave(id);
        }
      } catch {
        setActionError("グループを更新できませんでした。もう一度お試しください。");
      } finally {
        setPending((prev) => {
          const next = { ...prev };
          delete next[id];
          return next;
        });
      }
    },
    [onJoin, onLeave],
  );

  const locationStale = groups.status === "error" && groups.error.code === "location_stale";
  const groupsFailed = groups.status === "error" && !locationStale;
  const focused = focusedId ? bubbles.get(focusedId) : undefined;
  const lonely = model.ring.length === 0 && model.nodes.length === 0;

  return (
    <AppShell tab="nearby">
      <ScreenHeader
        title="ちかく"
        trailing={
          <span className={styles.headerActions}>
            <PressableLink className={styles.headerLink} href="/notifications/" aria-label="通知">
              <Icon name="notifications" size={22} />
            </PressableLink>
            <PressableLink className={styles.headerLink} href="/encounters/" aria-label="すれ違い">
              <Icon name="near_me" size={22} />
            </PressableLink>
            <PressableLink
              className={styles.headerLink}
              href="/group/graph/"
              aria-label="グループの関係"
            >
              <Icon name="account_tree" size={22} />
            </PressableLink>
          </span>
        }
      />

      <div className={styles.stage}>
        <BubbleCanvas
          className={styles.canvas}
          items={items}
          itemSize={68}
          spread={0.25}
          focusScale={1.25}
          rotation={-(heading ?? 0)}
          ariaLabel="近くの人とグループ"
          onFocusChange={setFocusedId}
          focusOnPress={(item) => item.bubble.kind !== "plus"}
          onItemPress={(item) => {
            const href = hrefOf(item.bubble);
            if (href) router.push(href);
          }}
          renderItem={(item, state) => (
            <NearbyBubble
              bubble={item.bubble}
              focused={state.focused}
              delay={item.delay}
              paused={!splashDone}
            />
          )}
        />

        {/* 下はタブバー上の共通バナー (ProximityDriver) と重なるので、カードと案内は上に出す */}
        <div className={styles.overlayTop}>
          {focused && focused.kind !== "me" && focused.kind !== "plus" ? (
            <FocusCard
              bubble={focused}
              pending={focused.kind === "group" ? pending[focused.groupId] : undefined}
              onOpen={() => {
                const href = hrefOf(focused);
                if (href) router.push(href);
              }}
              onToggleMembership={(group) =>
                void run(group.groupId, group.isMember ? "leave" : "join")
              }
            />
          ) : lonely ? (
            <p className={styles.hint}>まだ近くに誰もいません。＋ からグループを作れます</p>
          ) : null}
          {locationStale && (
            <div className={styles.banner}>
              <Notice tone="warning" icon="location_on">
                グループを見るには位置情報が必要です
              </Notice>
              {onEnableLocation && (
                <Button variant="secondary" size="sm" onClick={onEnableLocation}>
                  位置を有効にする
                </Button>
              )}
            </div>
          )}
          {groupsFailed && (
            <div className={styles.banner}>
              <Notice tone="warning" icon="cloud_off">
                近くのグループを読み込めませんでした
              </Notice>
              {onRetry && (
                <Button variant="secondary" size="sm" icon="refresh" onClick={onRetry}>
                  再読み込み
                </Button>
              )}
            </div>
          )}
          {actionError && (
            <p className={styles.actionError} role="alert">
              {actionError}
            </p>
          )}
        </div>

      </div>
    </AppShell>
  );
}

type FocusCardProps = {
  bubble: Exclude<NearbyBubbleModel, { kind: "me" } | { kind: "plus" }>;
  pending?: "join" | "leave";
  onOpen: () => void;
  onToggleMembership: (group: Extract<NearbyBubbleModel, { kind: "group" }>) => void;
};

/** 中央に来た泡の説明と操作。 */
function FocusCard({ bubble, pending, onOpen, onToggleMembership }: FocusCardProps) {
  let title: string;
  let detail: string;
  switch (bubble.kind) {
    case "party":
      title = bubble.name;
      detail = `パーティー・${bubble.memberCount}人`;
      break;
    case "group":
      title = bubble.name;
      detail = `${bubble.detail}・メンバー ${bubble.memberCount}人`;
      break;
    case "person":
      title = bubble.user.name;
      detail = [
        bubble.source === "friend" ? "フレンド" : bubble.source === "encounter" ? "すれ違い" : null,
        bubble.detail,
        bubble.groupName,
      ]
        .filter(Boolean)
        .join("・");
      break;
  }

  return (
    <div className={styles.focusCard}>
      <div className={styles.focusText}>
        <p className={styles.focusTitle}>{title}</p>
        <p className={styles.focusDetail}>{detail}</p>
      </div>
      {bubble.kind === "group" && bubble.inside && (
        <Button
          variant={bubble.isMember ? "secondary" : "primary"}
          size="sm"
          disabled={pending !== undefined}
          onClick={() => onToggleMembership(bubble)}
        >
          {pending === "join"
            ? "参加中…"
            : pending === "leave"
              ? "退出中…"
              : bubble.isMember
                ? "退出"
                : "参加"}
        </Button>
      )}
      <Button variant="secondary" size="sm" onClick={onOpen}>
        見る
      </Button>
    </div>
  );
}
