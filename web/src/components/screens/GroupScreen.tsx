"use client";

import { useMemo, useState, type FormEvent, type ReactNode } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import type {
  Group,
  GroupMember,
  GroupVisibility,
  Pin,
  Post,
} from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { PinList } from "@/components/feature/pin";
import { PinnedPostCard, PostCard } from "@/components/feature/post";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Fab } from "@/components/ui/Fab";
import { Icon } from "@/components/ui/Icon";
import { IconButton } from "@/components/ui/IconButton";
import { List, ListItem } from "@/components/ui/List";
import { Notice } from "@/components/ui/Notice";
import { SectionHeader } from "@/components/ui/SectionHeader";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Tag } from "@/components/ui/Tag";
import styles from "./GroupScreen.module.css";

type GroupTab = "timeline" | "pin" | "media" | "members";

const TABS: { value: GroupTab; label: string }[] = [
  { value: "timeline", label: "タイムライン" },
  { value: "pin", label: "ピン" },
  { value: "media", label: "メディア" },
  { value: "members", label: "メンバー" },
];

const EMPTY_LABEL: Record<GroupTab, string> = {
  timeline: "",
  pin: "まだピンされた投稿はありません",
  media: "まだメディアはありません",
  members: "まだメンバーはいません",
};

/** メンバーの役割 (09 §4) の表示ラベル。 */
const ROLE_LABEL: Record<string, string> = {
  owner: "オーナー",
  member: "メンバー",
};

const VISIBILITY_OPTIONS: { value: GroupVisibility; label: string }[] = [
  { value: "open", label: "オープン" },
  { value: "closed", label: "招待制" },
];

const MAX_NAME = 40;
const MAX_DESCRIPTION = 500;

/** PATCH /groups/{id} (09 §4) に送る変更。未指定の項目は変更しない。 */
export type GroupUpdateDraft = {
  name: string;
  description: string;
  visibility: GroupVisibility;
  /** 招待制のときだけ送る承認人数 (1 以上)。 */
  inviteQuorum?: number;
};

/** 編集の送信結果。unavailable はエンドポイント未対応 (404・405・501)。 */
export type GroupSaveResult =
  | { status: "ok" }
  | { status: "unavailable" }
  | { status: "error"; message: string; fields?: Record<string, string> };

function roleLabel(role: string): string {
  return ROLE_LABEL[role] ?? role;
}

export type GroupScreenProps = {
  /**
   * GET /groups/{id} の取得状態。指定時はこのデータを描画の source にする
   * (loading / error / unavailable はヘッダー下にインライン表示する)。
   */
  state?: ResourceState<Group>;
  /** state 未指定時に使う取得済みグループ (返信画面のオーバーレイなど)。 */
  group?: Group;
  /** タイムラインに並べる投稿 (ドメイン変換済み)。 */
  posts: Post[];
  /**
   * GET /instances/{spaceId}/posts の取得状態。未指定なら posts をそのまま ok として描画する。
   * loading / error / unavailable はタイムライン内にインライン表示する。
   */
  postsState?: ResourceState<Post[]>;
  /** 続きのページがあるか (usePagedResource.hasMore)。 */
  hasMore?: boolean;
  /** 続きの取得中か (usePagedResource.loadingMore)。 */
  loadingMore?: boolean;
  /** 次のページを末尾に足す (usePagedResource.loadMore)。 */
  onLoadMore?: () => void;
  /** グループ取得の再実行 (useResource.reload)。 */
  groupRetry?: () => void;
  /** 投稿一覧の再実行 (usePagedResource.reload)。 */
  postsRetry?: () => void;
  /** Space の Pin 一覧 (GET /instances/{spaceId}/pins, 10 §6)。 */
  pins?: Pin[];
  /** Pin 一覧の取得状態。 */
  pinsState?: ResourceState<Pin[]>;
  /** Pin 一覧の再実行 (useResource.reload)。 */
  pinsRetry?: () => void;
  /** Pin / 解除の送信中。 */
  pinBusy?: boolean;
  /** 投稿を手動 Pin する。 */
  onPin?: (post: Post) => void;
  /** Pin を外す。 */
  onUnpin?: (pin: Pin) => void;
  /** Pin / 解除の直近の失敗。 */
  pinError?: string | null;
  /** メンバー一覧 (GET /groups/{id}/members, 09 §4)。メンバーのときだけ渡る。 */
  members?: GroupMember[];
  /** メンバー一覧の取得状態。 */
  membersState?: ResourceState<GroupMember[]>;
  /** メンバー一覧の再実行 (useResource.reload)。 */
  membersRetry?: () => void;
  /**
   * PATCH /groups/{id} (09 §4)。owner のときだけ渡す。成功で resolve する。
   * 未指定なら編集 UI を出さない (無言の no-op にしない)。
   */
  onSave?: (draft: GroupUpdateDraft) => Promise<GroupSaveResult>;
  tabBar?: boolean;
  overlay?: ReactNode;
  onCompose?: (group: Group) => void;
  onReply?: (post: Post) => void;
};

export function GroupScreen({
  state,
  group: groupProp,
  posts,
  postsState,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  groupRetry,
  postsRetry,
  pins = [],
  pinsState,
  pinsRetry,
  pinBusy = false,
  onPin,
  onUnpin,
  pinError,
  members,
  membersState,
  membersRetry,
  onSave,
  tabBar = true,
  overlay,
  onCompose,
  onReply,
}: GroupScreenProps) {
  const router = useRouter();
  const [tab, setTab] = useState<GroupTab>("timeline");
  const [editing, setEditing] = useState(false);
  const [saveNotice, setSaveNotice] = useState<string | null>(null);

  // state があればそれを、無ければ group をそのまま ok として扱う (返信画面の互換)。
  const groupState: ResourceState<Group> =
    state ?? (groupProp ? { status: "ok", data: groupProp } : { status: "loading" });

  const { pinned, rest } = useMemo(() => {
    const pinnedPosts: Post[] = [];
    const restPosts: Post[] = [];
    for (const post of posts) {
      (post.pinned ? pinnedPosts : restPosts).push(post);
    }
    return { pinned: pinnedPosts, rest: restPosts };
  }, [posts]);

  if (groupState.status !== "ok") {
    return (
      <AppShell tab={tabBar ? "nearby" : undefined}>
        <ScreenHeader
          title="グループ"
          back
          trailing={<IconButton icon="more_horiz" label="グループのメニュー" />}
        />
        <div className={styles.gutter}>
          {groupState.status === "loading" ? (
            <Card padding="md" className={styles.empty}>
              読み込み中…
            </Card>
          ) : groupState.status === "error" ? (
            <Card padding="md" className={styles.stack}>
              <p className={styles.empty}>グループを読み込めませんでした</p>
              {groupRetry && (
                <Button variant="secondary" icon="refresh" onClick={groupRetry}>
                  再読み込み
                </Button>
              )}
            </Card>
          ) : (
            // empty / unavailable (404・405・501) は「まだ無い / 未対応」。
            <Card padding="md" className={styles.empty}>
              グループが見つかりません
            </Card>
          )}
        </div>
        {overlay}
      </AppShell>
    );
  }

  const group = groupState.data;
  const canEdit = group.role === "owner" && onSave !== undefined;
  const isMember = group.insideArea;

  const goCompose = () => {
    onCompose?.(group);
    router.push(`/compose/?target=group:${group.id}`);
  };

  const goReply = (post: Post) => {
    onReply?.(post);
    router.push(`/group/replies/?id=${group.id}&post=${post.id}`);
  };

  const postsStatus = postsState?.status ?? "ok";
  const pinState: ResourceState<Pin[]> =
    pinsState ?? { status: "ok", data: pins };
  const membersStatus = membersState?.status ?? "ok";
  const memberList = members ?? [];

  return (
    <AppShell tab={tabBar ? "nearby" : undefined}>
      <ScreenHeader
        title={group.name}
        back
        trailing={
          <span className={styles.headerActions}>
            {canEdit && (
              <IconButton
                icon="edit"
                label="グループを編集"
                onClick={() => {
                  setSaveNotice(null);
                  setEditing(true);
                }}
              />
            )}
            {group.visibility === "closed" && (
              <Link
                className={styles.headerLink}
                href={`/group/invites/?id=${group.id}`}
                aria-label="グループの招待"
              >
                <Icon name="group_add" size={22} />
              </Link>
            )}
            <IconButton icon="more_horiz" label="グループのメニュー" />
          </span>
        }
      />

      <div className={styles.tags}>
        <Tag label={group.visibility === "open" ? "オープン" : "招待制"} />
        <Tag label={`${group.memberCount}人`} />
        <Tag label={group.areaLabel} />
      </div>

      {isMember && (
        <div className={styles.gutter}>
          <nav className={styles.links} aria-label="グループのリンク">
            <Link className={styles.link} href={`/group/invites/?id=${group.id}`}>
              招待
            </Link>
            <Link className={styles.link} href={`/group/votes/?id=${group.id}`}>
              投票
            </Link>
            <Link className={styles.link} href={`/group/photo/?id=${group.id}`}>
              写真を共有
            </Link>
          </nav>
        </div>
      )}

      {saveNotice && (
        <div className={styles.gutter}>
          <Notice tone="info" icon="check_circle">
            {saveNotice}
          </Notice>
        </div>
      )}

      <div className={styles.gutter}>
        <Notice tone="warning" icon="location_on">
          {group.insideArea
            ? "エリアに入りました・エリア外では投稿できません"
            : "エリア外では投稿できません"}
        </Notice>
      </div>

      <div className={styles.gutter}>
        <SegmentedControl options={TABS} value={tab} onChange={setTab} />
      </div>

      {tab === "timeline" ? (
        <div className={styles.timeline}>
          {postsStatus === "loading" ? (
            <Card padding="md" className={styles.empty}>
              読み込み中…
            </Card>
          ) : postsStatus === "error" ? (
            <Card padding="md" className={styles.stack}>
              <p className={styles.empty}>投稿を読み込めませんでした</p>
              {postsRetry && (
                <Button variant="secondary" icon="refresh" onClick={postsRetry}>
                  再読み込み
                </Button>
              )}
            </Card>
          ) : postsStatus === "unavailable" ? (
            // 404・405・501 は「まだ無い / 未対応」。
            <Card padding="md" className={styles.empty}>
              まだ投稿はありません
            </Card>
          ) : posts.length === 0 ? (
            <Card padding="md" className={styles.empty}>
              まだ投稿はありません
            </Card>
          ) : (
            <>
              {pinned.map((post) => (
                <PinnedPostCard key={post.id} post={post} />
              ))}
              {rest.map((post) => (
                <PostCard
                  key={post.id}
                  post={post}
                  onReply={goReply}
                />
              ))}
              {hasMore && (
                <div className={styles.more}>
                  <Button
                    variant="secondary"
                    onClick={onLoadMore}
                    disabled={loadingMore}
                  >
                    {loadingMore ? "読み込み中…" : "もっと見る"}
                  </Button>
                </div>
              )}
            </>
          )}
        </div>
      ) : tab === "pin" ? (
        <div className={styles.timeline}>
          {pinError && (
            <Notice tone="warning" icon="error">
              {pinError}
            </Notice>
          )}
          <PinList
            state={pinState}
            pins={pins}
            posts={posts}
            onPin={(post) => onPin?.(post)}
            onUnpin={(pin) => onUnpin?.(pin)}
            onRetry={pinsRetry}
            busy={pinBusy}
          />
        </div>
      ) : tab === "members" ? (
        <div className={styles.gutter}>
          <SectionHeader title="メンバー" />
          {membersStatus === "loading" ? (
            <Card padding="md" className={styles.empty}>
              読み込み中…
            </Card>
          ) : membersStatus === "error" ? (
            <Card padding="md" className={styles.stack}>
              <p className={styles.empty}>メンバーを読み込めませんでした</p>
              {membersRetry && (
                <Button variant="secondary" icon="refresh" onClick={membersRetry}>
                  再読み込み
                </Button>
              )}
            </Card>
          ) : membersStatus === "unavailable" ? (
            <Card padding="md" className={styles.empty}>
              メンバーは準備中です
            </Card>
          ) : memberList.length === 0 ? (
            <Card padding="md" className={styles.empty}>
              まだメンバーはいません
            </Card>
          ) : (
            <Card padding="md">
              <List>
                {memberList.map((member) => (
                  <ListItem
                    key={member.did}
                    leading={<Avatar user={member} size={40} />}
                    title={member.name}
                    subtitle={`@${member.handle}`}
                    trailing={<Tag label={roleLabel(member.role)} />}
                  />
                ))}
              </List>
            </Card>
          )}
        </div>
      ) : (
        <div className={styles.gutter}>
          <Card padding="md" className={styles.empty}>
            {EMPTY_LABEL[tab]}
          </Card>
        </div>
      )}

      {group.insideArea && <Fab label="投稿する" onClick={goCompose} />}
      {editing && canEdit && onSave && (
        <GroupEditDialog
          key={group.id}
          group={group}
          onSave={onSave}
          onSaved={() => {
            setSaveNotice("保存しました");
            setEditing(false);
          }}
          onClose={() => setEditing(false)}
        />
      )}
      {overlay}
    </AppShell>
  );
}

type GroupEditDialogProps = {
  group: Group;
  onSave: (draft: GroupUpdateDraft) => Promise<GroupSaveResult>;
  /** 保存に成功した。親がダイアログを閉じ、成功の通知を出す。 */
  onSaved: () => void;
  onClose: () => void;
};

/**
 * グループ編集フォーム (09 §4)。開いている間だけ mount されるため、props を初期値に
 * した useState をそのまま使える (effect 内 setState を避ける keyed-state パターン)。
 * geofence は backend が変更を受け付けないので入力を持たない。
 */
function GroupEditDialog({ group, onSave, onSaved, onClose }: GroupEditDialogProps) {
  const [name, setName] = useState(group.name);
  const [description, setDescription] = useState(group.description);
  const [visibility, setVisibility] = useState<GroupVisibility>(group.visibility);
  const [quorum, setQuorum] = useState(String(group.inviteQuorum ?? 1));
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [fields, setFields] = useState<Record<string, string>>({});

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (saving) {
      return;
    }
    const trimmed = name.trim();
    if (trimmed === "") {
      setFields({ name: "必須です" });
      setMessage(null);
      return;
    }
    const draft: GroupUpdateDraft = { name: trimmed, description, visibility };
    if (visibility === "closed") {
      draft.inviteQuorum = Math.max(1, Number.parseInt(quorum, 10) || 1);
    }
    setSaving(true);
    setMessage(null);
    setFields({});
    void onSave(draft)
      .then((result) => {
        if (result.status === "ok") {
          onSaved();
          return;
        }
        if (result.status === "unavailable") {
          setMessage("グループの編集はまだ利用できません。");
          return;
        }
        setFields(result.fields ?? {});
        setMessage(result.message);
      })
      .finally(() => {
        setSaving(false);
      });
  };

  return (
    <div className={styles.overlay}>
      <div
        className={styles.dialog}
        role="dialog"
        aria-modal="true"
        aria-label="グループを編集"
      >
        <header className={styles.dialogHeader}>
          <span className={styles.dialogTitle}>グループを編集</span>
          <button
            type="button"
            className={styles.dialogClose}
            aria-label="閉じる"
            onClick={onClose}
          >
            <Icon name="close" size={22} />
          </button>
        </header>

        <form className={styles.form} onSubmit={submit}>
          <label className={styles.fieldLabel} htmlFor="group-edit-name">
            グループ名
          </label>
          <input
            id="group-edit-name"
            className={styles.input}
            value={name}
            maxLength={MAX_NAME}
            disabled={saving}
            onChange={(event) => setName(event.target.value)}
          />
          {fields.name && (
            <p className={styles.fieldError} role="alert">
              {fields.name}
            </p>
          )}

          <label className={styles.fieldLabel} htmlFor="group-edit-description">
            説明
          </label>
          <textarea
            id="group-edit-description"
            className={styles.textarea}
            value={description}
            maxLength={MAX_DESCRIPTION}
            rows={3}
            disabled={saving}
            onChange={(event) => setDescription(event.target.value)}
          />
          {fields.description && (
            <p className={styles.fieldError} role="alert">
              {fields.description}
            </p>
          )}

          <span className={styles.fieldLabel}>公開範囲</span>
          <SegmentedControl
            options={VISIBILITY_OPTIONS}
            value={visibility}
            onChange={setVisibility}
          />

          {visibility === "closed" && (
            <>
              <label className={styles.fieldLabel} htmlFor="group-edit-quorum">
                承認が必要な人数
              </label>
              <input
                id="group-edit-quorum"
                className={styles.input}
                type="number"
                min={1}
                value={quorum}
                disabled={saving}
                onChange={(event) => setQuorum(event.target.value)}
              />
              {fields.inviteQuorum && (
                <p className={styles.fieldError} role="alert">
                  {fields.inviteQuorum}
                </p>
              )}
            </>
          )}

          {message && (
            <Notice tone="warning" icon="error">
              {message}
            </Notice>
          )}

          <div className={styles.formActions}>
            <Button variant="secondary" onClick={onClose} disabled={saving}>
              閉じる
            </Button>
            <Button variant="primary" type="submit" disabled={saving}>
              {saving ? "保存中…" : "保存"}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}
