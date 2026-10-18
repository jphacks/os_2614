"use client";

import {
  forwardRef,
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type CSSProperties,
  type ReactNode,
} from "react";
import type { Memory, Profile, Stats } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import {
  ACCEPTED_MEDIA_TYPES,
  validateMediaFile,
} from "@/components/feature/compose";
import { AppShell } from "@/components/layout/AppShell";
import { ImageCropDialog, type ImageCropKind } from "@/components/feature/imageCrop";
import { MemoryThumbCard } from "@/components/feature/memory";
import { PublicPostsSection } from "@/components/feature/post";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { IconButton } from "@/components/ui/IconButton";
import { List, ListItem } from "@/components/ui/List";
import { Notice } from "@/components/ui/Notice";
import { PressableLink } from "@/components/ui/Pressable";
import { SectionHeader } from "@/components/ui/SectionHeader";
import { StatTriple } from "@/components/ui/StatTriple";
import styles from "./ProfileScreen.module.css";

/** PUT /me/status (13 §1) の結果。 */
export type StatusSaveResult =
  | { status: "ok"; value: string }
  | { status: "unavailable" }
  | { status: "error"; message: string; reauth?: boolean };

export type ImageDraft =
  | { kind: "keep" }
  | { kind: "set"; blob: Blob; previewUrl: string }
  | { kind: "remove" };

/** プロフィール編集画面が保持する未保存値。 */
export type ProfileDraft = {
  displayName: string;
  description: string;
  initialDisplayName: string;
  initialDescription: string;
  avatar: ImageDraft;
  banner: ImageDraft;
};

/** PATCH /me/profile と画像 API (13 §1.3) の共通結果。 */
export type ProfileActionResult =
  | { status: "ok" }
  | { status: "unavailable" }
  | {
      status: "error";
      message: string;
      reauth?: boolean;
      conflict?: boolean;
      remaining?: ProfileDraft;
    };

/** displayName の上限 (service.MaxDisplayNameGraphemes、13 §1.3)。 */
export const MAX_DISPLAY_NAME_GRAPHEMES = 64;

/** description の上限 (service.MaxProfileDescriptionGraphemes、13 §1.3)。 */
export const MAX_PROFILE_DESCRIPTION_GRAPHEMES = 256;

const GRAPHEME_JOINERS = /[\p{Mn}\p{Me}\p{Cf}]/u;

// backend internal/utils/text.go の GraphemeCount と同じ数え方にする。結合文字・
// 書式文字 (ZWJ を含む) と肌色の絵文字修飾子は前の文字に付くものとして数えない。
function graphemeCount(text: string): number {
  let count = 0;
  for (const char of text) {
    if (GRAPHEME_JOINERS.test(char)) {
      continue;
    }
    const codePoint = char.codePointAt(0) ?? 0;
    if (codePoint >= 0x1f3fb && codePoint <= 0x1f3ff) {
      continue;
    }
    count += 1;
  }
  return count;
}

export type ProfileScreenProps = {
  /** GET /me の取得状態。 */
  state: ResourceState<Profile>;
  /** GET /me/stats の取得状態。未取得時は統計を `—` で表示する。 */
  stats: ResourceState<Stats>;
  /** GET /memories の最初のページ。 */
  memories: ResourceState<Memory[]>;
  /** error 状態の再取得。 */
  onRetry?: () => void;
  /** ステータスの保存 (PUT /me/status, 13 §1)。 */
  onSaveStatus?: (status: string) => Promise<StatusSaveResult>;
  /** 名前・自己紹介・画像の保存を順番に行う。 */
  onSaveProfile?: (draft: ProfileDraft) => Promise<ProfileActionResult>;
  /** atproto の再認証が必要なときのログイン導線。 */
  onRelogin?: () => void;
};

export function ProfileScreen({
  state,
  stats,
  memories,
  onRetry,
  onSaveStatus,
  onSaveProfile,
  onRelogin,
}: ProfileScreenProps) {
  return (
    <AppShell tab="profile">
      <div className={styles.content}>
        {state.status === "ok" ? (
          <ProfileBody
            me={state.data}
            stats={stats}
            memories={memories}
            onSaveStatus={onSaveStatus}
            onSaveProfile={onSaveProfile}
            onRelogin={onRelogin}
          />
        ) : state.status === "loading" ? (
          <ProfileSkeleton />
        ) : state.status === "error" ? (
          <ProfileState
            icon="cloud_off"
            title="プロフィールを読み込めませんでした"
            description="通信の状態を確認して、もう一度お試しください。"
            action={
              onRetry ? (
                <Button variant="secondary" icon="refresh" onClick={onRetry}>
                  再読み込み
                </Button>
              ) : undefined
            }
          />
        ) : (
          // empty / unavailable (404・405・501) は「まだ無い / 未対応」。
          <ProfileState
            icon="construction"
            title="プロフィールは準備中です"
            description="この機能はまだ提供されていません。"
          />
        )}
      </div>
    </AppShell>
  );
}

type ProfileBodyProps = {
  me: Profile;
  stats: ResourceState<Stats>;
  memories: ResourceState<Memory[]>;
  onSaveStatus?: (status: string) => Promise<StatusSaveResult>;
  onSaveProfile?: (draft: ProfileDraft) => Promise<ProfileActionResult>;
  onRelogin?: () => void;
};

function newDraft(me: Profile): ProfileDraft {
  return {
    displayName: me.name,
    description: me.bio,
    initialDisplayName: me.name,
    initialDescription: me.bio,
    avatar: { kind: "keep" },
    banner: { kind: "keep" },
  };
}

function ProfileBody({
  me,
  stats,
  memories,
  onSaveStatus,
  onSaveProfile,
  onRelogin,
}: ProfileBodyProps) {
  const [statusEditing, setStatusEditing] = useState(false);
  const [statusDraft, setStatusDraft] = useState(me.status ?? "");
  const [statusSaving, setStatusSaving] = useState(false);
  const [statusError, setStatusError] = useState<string | null>(null);
  const [statusReauth, setStatusReauth] = useState(false);
  const [profileEditing, setProfileEditing] = useState(false);
  const [draft, setDraft] = useState<ProfileDraft>(() => newDraft(me));
  const [profileSaving, setProfileSaving] = useState(false);
  const [profileError, setProfileError] = useState<string | null>(null);
  const [profileReauth, setProfileReauth] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [crop, setCrop] = useState<{
    kind: ImageCropKind;
    file: File;
  } | null>(null);
  const [menu, setMenu] = useState<ImageCropKind | null>(null);
  const avatarInput = useRef<HTMLInputElement>(null);
  const bannerInput = useRef<HTMLInputElement>(null);
  const statusInputRef = useRef<HTMLInputElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const objectUrls = useRef<string[]>([]);

  useEffect(() => {
    const closeOnOutside = (event: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setMenu(null);
      }
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setMenu(null);
      }
    };
    document.addEventListener("mousedown", closeOnOutside);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("mousedown", closeOnOutside);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, []);

  useEffect(
    () => () => {
      objectUrls.current.forEach((url) => URL.revokeObjectURL(url));
    },
    [],
  );

  useEffect(() => {
    if (statusEditing) {
      statusInputRef.current?.focus();
    }
  }, [statusEditing]);

  const revokePreviewUrl = (url: string) => {
    URL.revokeObjectURL(url);
    objectUrls.current = objectUrls.current.filter((item) => item !== url);
  };

  const startStatusEdit = () => {
    setProfileEditing(false);
    setStatusDraft(me.status ?? "");
    setStatusError(null);
    setStatusReauth(false);
    setStatusEditing(true);
  };

  const startProfileEdit = () => {
    setStatusEditing(false);
    setDraft(newDraft(me));
    setProfileError(null);
    setProfileReauth(false);
    setNotice(null);
    setProfileEditing(true);
  };

  const cancelProfileEdit = () => {
    for (const image of [draft.avatar, draft.banner]) {
      if (image.kind === "set") {
        revokePreviewUrl(image.previewUrl);
      }
    }
    setProfileEditing(false);
    setProfileError(null);
    setProfileReauth(false);
    setMenu(null);
  };

  const chooseImage = (kind: ImageCropKind) => {
    setMenu(null);
    (kind === "avatar" ? avatarInput : bannerInput).current?.click();
  };

  const onFileSelected = (
    kind: ImageCropKind,
    event: ChangeEvent<HTMLInputElement>,
  ) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) {
      return;
    }
    const check = validateMediaFile(file);
    if (!check.ok) {
      setProfileError(check.message);
      return;
    }
    setCrop({ kind, file });
  };

  const setCroppedImage = (kind: ImageCropKind, blob: Blob) => {
    const previewUrl = URL.createObjectURL(blob);
    objectUrls.current.push(previewUrl);
    setDraft((current) => {
      const previous = current[kind];
      if (previous.kind === "set") {
        revokePreviewUrl(previous.previewUrl);
      }
      return {
        ...current,
        [kind]: { kind: "set", blob, previewUrl },
      };
    });
    setCrop(null);
  };

  const removeImage = (kind: ImageCropKind) => {
    setMenu(null);
    setDraft((current) => {
      const previous = current[kind];
      if (previous.kind === "set") {
        revokePreviewUrl(previous.previewUrl);
      }
      return { ...current, [kind]: { kind: "remove" } };
    });
  };

  const imagePreview = (kind: ImageCropKind) => {
    const image = draft[kind];
    if (image.kind === "set") {
      return image.previewUrl;
    }
    if (image.kind === "remove") {
      return undefined;
    }
    return kind === "avatar" ? me.avatarUrl : me.bannerUrl;
  };

  const saveStatus = async () => {
    if (!onSaveStatus || statusSaving || Array.from(statusDraft).length > 40) {
      return;
    }
    setStatusSaving(true);
    setStatusError(null);
    setStatusReauth(false);
    const result = await onSaveStatus(statusDraft);
    setStatusSaving(false);
    if (result.status === "ok") {
      setStatusEditing(false);
      return;
    }
    if (result.status === "error") {
      setStatusReauth(result.reauth === true);
    }
    setStatusError(
      result.status === "unavailable"
        ? "ステータスはまだ更新できません。"
        : result.message,
    );
  };

  const saveProfile = async () => {
    if (!onSaveProfile || profileSaving) {
      return;
    }
    const nameCount = graphemeCount(draft.displayName);
    const bioCount = graphemeCount(draft.description);
    if (nameCount > MAX_DISPLAY_NAME_GRAPHEMES) {
      setProfileError("名前は64文字までです");
      return;
    }
    if (bioCount > MAX_PROFILE_DESCRIPTION_GRAPHEMES) {
      setProfileError("自己紹介は256文字までです");
      return;
    }
    setProfileSaving(true);
    setProfileError(null);
    setProfileReauth(false);
    const result = await onSaveProfile({
      ...draft,
      displayName: draft.displayName.trim(),
      description: draft.description.trim(),
    });
    setProfileSaving(false);
    if (result.status === "ok") {
      setProfileEditing(false);
      setNotice("プロフィールを更新しました");
      return;
    }
    if (result.status === "error" && result.remaining) {
      setDraft(result.remaining);
    }
    if (result.status === "error") {
      setProfileReauth(result.reauth === true);
    }
    setProfileError(
      result.status === "unavailable"
        ? "プロフィールはまだ更新できません。"
        : result.message,
    );
  };

  const statValue = (value: number | undefined) =>
    stats.status === "ok" ? value ?? 0 : "—";
  const avatarUrl = imagePreview("avatar");
  const bannerUrl = imagePreview("banner");
  const statusCount = Array.from(statusDraft).length;

  return (
    <>
      <input
        ref={avatarInput}
        type="file"
        accept={ACCEPTED_MEDIA_TYPES.join(",")}
        hidden
        data-testid="profile-avatar-input"
        onChange={(event) => onFileSelected("avatar", event)}
      />
      <input
        ref={bannerInput}
        type="file"
        accept={ACCEPTED_MEDIA_TYPES.join(",")}
        hidden
        data-testid="profile-banner-input"
        onChange={(event) => onFileSelected("banner", event)}
      />

      <div className={styles.bannerWrap}>
        {profileEditing ? (
          <button
            type="button"
            className={styles.banner}
            onClick={() => (bannerUrl ? setMenu("banner") : chooseImage("banner"))}
            aria-label="バナーを変更"
          >
            <BannerImage url={bannerUrl} editable />
          </button>
        ) : (
          <div className={styles.banner} aria-hidden>
            <BannerImage url={me.bannerUrl} />
          </div>
        )}
        {profileEditing && menu === "banner" && (
          <ImageMenu
            ref={menuRef}
            className={styles.bannerMenu}
            onChange={() => chooseImage("banner")}
            onRemove={() => removeImage("banner")}
          />
        )}

        {profileEditing ? (
          <button
            type="button"
            className={styles.avatar}
            onClick={() => (avatarUrl ? setMenu("avatar") : chooseImage("avatar"))}
            aria-label="アイコンを変更"
          >
            <AvatarImage me={me} url={avatarUrl} editable />
          </button>
        ) : (
          <span className={styles.avatar}>
            <Avatar user={me} size={80} />
          </span>
        )}
        {profileEditing && menu === "avatar" && (
          <ImageMenu
            ref={menuRef}
            className={styles.avatarMenu}
            onChange={() => chooseImage("avatar")}
            onRemove={() => removeImage("avatar")}
          />
        )}

        {!profileEditing && (
          <div className={styles.editButton}>
            <Button variant="secondary" size="sm" icon="edit" onClick={startProfileEdit}>
              編集
            </Button>
          </div>
        )}
      </div>

      <div className={`${styles.nameRow} ${statusEditing ? styles.nameRowEditing : ""}`}>
        <h1 className={styles.name}>{me.name}</h1>
        {!profileEditing && statusEditing ? (
          <div className={styles.statusEditRow}>
            <input
              ref={statusInputRef}
              className={styles.statusInput}
              value={statusDraft}
              aria-label="ステータス"
              disabled={statusSaving}
              onChange={(event) => setStatusDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Escape") {
                  event.stopPropagation();
                  setStatusEditing(false);
                } else if (
                  event.key === "Enter" &&
                  !event.nativeEvent.isComposing &&
                  event.nativeEvent.keyCode !== 229
                ) {
                  event.preventDefault();
                  void saveStatus();
                }
              }}
            />
            <span
              className={
                statusCount > 40
                  ? `${styles.statusCounter} ${styles.statusCounterOver}`
                  : styles.statusCounter
              }
            >
              {statusCount}/40
            </span>
            <IconButton
              icon="close"
              label="キャンセル"
              onClick={() => setStatusEditing(false)}
              disabled={statusSaving}
            />
            <IconButton
              icon="check"
              label="保存"
              onClick={() => void saveStatus()}
              disabled={statusSaving || statusCount > 40}
            />
          </div>
        ) : !profileEditing ? (
          <StatusPill status={me.status ?? ""} onEdit={startStatusEdit} />
        ) : null}
      </div>

      {statusEditing && statusError && (
        <div role="alert">
          <Notice tone="warning" icon="error">
            {statusError}
            {statusReauth && onRelogin && (
              <Button variant="secondary" size="sm" onClick={onRelogin}>
                再ログイン
              </Button>
            )}
          </Notice>
        </div>
      )}

      <p className={styles.bio}>{me.bio}</p>

      {me.pdsHost && (
        <p className={styles.pdsHost}>
          あなたのデータは <strong>{me.pdsHost}</strong> にあります。
          <PressableLink href="/settings/data/" className={styles.pdsHostLink}>
            データの置き場所
          </PressableLink>
        </p>
      )}

      {notice && (
        <div role="status">
          <Notice tone="info" icon="check">
            {notice}
          </Notice>
        </div>
      )}

      {profileEditing && (
        <div className={styles.profileEditor}>
          <label className={styles.fieldLabel} htmlFor="profile-display-name">
            名前
          </label>
          <input
            id="profile-display-name"
            className={styles.fieldInput}
            value={draft.displayName}
            disabled={profileSaving}
            onChange={(event) =>
              setDraft((current) => ({
                ...current,
                displayName: event.target.value,
              }))
            }
          />
          <span
            className={
              graphemeCount(draft.displayName) > MAX_DISPLAY_NAME_GRAPHEMES
                ? styles.counterOver
                : styles.counter
            }
          >
            {graphemeCount(draft.displayName)}/{MAX_DISPLAY_NAME_GRAPHEMES}
          </span>

          <label className={styles.fieldLabel} htmlFor="profile-description">
            自己紹介
          </label>
          <textarea
            id="profile-description"
            className={styles.fieldTextarea}
            value={draft.description}
            disabled={profileSaving}
            onChange={(event) =>
              setDraft((current) => ({
                ...current,
                description: event.target.value,
              }))
            }
          />
          <span
            className={
              graphemeCount(draft.description) > MAX_PROFILE_DESCRIPTION_GRAPHEMES
                ? styles.counterOver
                : styles.counter
            }
          >
            {graphemeCount(draft.description)}/{MAX_PROFILE_DESCRIPTION_GRAPHEMES}
          </span>

          <div className={styles.statusActions}>
            <Button
              variant="secondary"
              size="sm"
              onClick={cancelProfileEdit}
              disabled={profileSaving}
            >
              キャンセル
            </Button>
            <Button
              variant="primary"
              size="sm"
              onClick={() => void saveProfile()}
              disabled={profileSaving}
            >
              {profileSaving ? "保存しています…" : "保存"}
            </Button>
          </div>
          {profileError && (
            <div role="alert">
              <Notice tone="warning" icon="error">
                {profileError}
                {profileReauth && onRelogin && (
                  <Button variant="secondary" size="sm" onClick={onRelogin}>
                    再ログイン
                  </Button>
                )}
              </Notice>
            </div>
          )}
        </div>
      )}

      <StatTriple
        items={[
          {
            value: statValue(stats.status === "ok" ? stats.data.encounters.total : undefined),
            label: "すれ違い",
          },
          {
            value: statValue(stats.status === "ok" ? stats.data.friends : undefined),
            label: "友達",
          },
          {
            value: statValue(stats.status === "ok" ? stats.data.parties.total : undefined),
            label: "パーティ",
          },
        ]}
      />

      <section className={styles.section}>
        <SectionHeader
          title="メモリー"
          action={{ label: "すべて見る ›", href: "/memory/" }}
        />
        <div className={styles.memories}>
          {memories.status === "ok" && memories.data.length > 0 ? (
            memories.data.map((memory) => (
              <MemoryThumbCard key={memory.spaceId} memory={memory} />
            ))
          ) : (
            <p className={styles.memoryEmpty}>
              {memories.status === "loading" ? "" : "まだメモリーはありません"}
            </p>
          )}
        </div>
      </section>

      <PublicPostsSection did={me.did} />

      <Card padding="md">
        <List>
          <ListItem
            leading={<Icon name="tune" size={22} />}
            title="設定"
            href="/settings/"
            trailing={<Icon name="chevron_right" size={20} />}
          />
          <ListItem
            leading={<Icon name="insights" size={22} />}
            title="これまでの統計"
            href="/stats/"
            trailing={<Icon name="chevron_right" size={20} />}
          />
        </List>
      </Card>

      {crop && (
        <ImageCropDialog
          kind={crop.kind}
          file={crop.file}
          onCancel={() => setCrop(null)}
          onConfirm={(blob) => setCroppedImage(crop.kind, blob)}
        />
      )}
    </>
  );
}

function StatusPill({ status, onEdit }: { status: string; onEdit: () => void }) {
  const viewportRef = useRef<HTMLSpanElement>(null);
  const textRef = useRef<HTMLSpanElement>(null);
  const [overflow, setOverflow] = useState(0);

  useEffect(() => {
    const viewport = viewportRef.current;
    const text = textRef.current;
    if (!viewport || !text) {
      return;
    }
    const measure = () => {
      setOverflow(Math.max(0, text.scrollWidth - viewport.clientWidth));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(viewport);
    observer.observe(text);
    return () => observer.disconnect();
  }, [status]);

  const marqueeStyle = {
    "--marquee-distance": `${overflow}px`,
    "--marquee-duration": `${Math.max(4, overflow / 35 / 0.3)}s`,
  } as CSSProperties;

  return (
    <span className={`${styles.status} ${!status ? styles.statusMuted : ""}`}>
      {status && <span className={styles.onlineDot} aria-hidden />}
      <span
        ref={viewportRef}
        className={styles.statusViewport}
        data-marquee={overflow > 0 ? "on" : "off"}
        title={status}
      >
        <span
          ref={textRef}
          className={styles.statusText}
          style={marqueeStyle}
        >
          {status || "ステータスを設定"}
        </span>
      </span>
      <IconButton icon="edit" label="ステータスを編集" onClick={onEdit} />
    </span>
  );
}

/* eslint-disable @next/next/no-img-element */
function BannerImage({ url, editable = false }: { url?: string; editable?: boolean }) {
  return url ? (
    <>
      <img className={styles.bannerImage} src={url} alt="" />
      {editable && (
        <span className={styles.imageOverlay} data-testid="profile-image-overlay">
          <Icon name="photo_camera" size={24} />
        </span>
      )}
    </>
  ) : (
    <>
      <span className={`${styles.cloud} ${styles.cloudA}`} />
      <span className={`${styles.cloud} ${styles.cloudB}`} />
      <span className={`${styles.cloud} ${styles.cloudC}`} />
      {editable && (
        <span className={styles.imageOverlay} data-testid="profile-image-overlay">
          <Icon name="photo_camera" size={24} />
        </span>
      )}
    </>
  );
}

function AvatarImage({ me, url, editable = false }: { me: Profile; url?: string; editable?: boolean }) {
  return url ? (
    <>
      <img className={styles.avatarImage} src={url} alt="" />
      {editable && (
        <span className={styles.imageOverlay} data-testid="profile-image-overlay">
          <Icon name="photo_camera" size={22} />
        </span>
      )}
    </>
  ) : (
    <>
      <Avatar user={{ ...me, avatarUrl: undefined }} size={80} />
      {editable && (
        <span className={styles.imageOverlay} data-testid="profile-image-overlay">
          <Icon name="photo_camera" size={22} />
        </span>
      )}
    </>
  );
}
/* eslint-enable @next/next/no-img-element */

type ImageMenuProps = {
  className: string;
  onChange: () => void;
  onRemove: () => void;
};

const ImageMenu = forwardRef<HTMLDivElement, ImageMenuProps>(function ImageMenu(
  { className, onChange, onRemove },
  ref,
) {
  return (
    <div ref={ref} className={`${styles.imageMenu} ${className}`}>
      <Button variant="ghost" size="sm" onClick={onChange}>
        画像を変更
      </Button>
      <Button variant="ghost" size="sm" onClick={onRemove}>
        削除
      </Button>
    </div>
  );
});

type ProfileStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function ProfileState({ icon, title, description, action }: ProfileStateProps) {
  return (
    <div className={styles.state}>
      <span className={styles.stateIcon} aria-hidden>
        <Icon name={icon} size={32} />
      </span>
      <p className={styles.stateTitle}>{title}</p>
      <p className={styles.stateDescription}>{description}</p>
      {action}
    </div>
  );
}

function ProfileSkeleton() {
  return (
    <div className={styles.skeleton} aria-hidden>
      <div className={styles.skeletonBanner} />
      <div className={styles.skeletonIdentity}>
        <span className={styles.skeletonAvatar} />
        <div className={styles.skeletonLines}>
          <span className={styles.skeletonLineShort} />
          <span className={styles.skeletonLineLong} />
        </div>
      </div>
      <div className={styles.skeletonBlock} />
    </div>
  );
}
