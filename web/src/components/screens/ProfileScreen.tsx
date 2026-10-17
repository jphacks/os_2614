"use client";

import { useRef, useState, type ChangeEvent, type ReactNode } from "react";
import type { Memory, Profile } from "@/domain/types";
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
import { SectionHeader } from "@/components/ui/SectionHeader";
import { StatTriple } from "@/components/ui/StatTriple";
import styles from "./ProfileScreen.module.css";

/** PUT /me/status (13 §1) の結果。 */
export type StatusSaveResult =
  | { status: "ok"; value: string }
  | { status: "unavailable" }
  | { status: "error"; message: string };

/** PATCH /me/profile と画像 API (13 §1.3) の共通結果。 */
export type ProfileActionResult =
  | { status: "ok" }
  | { status: "unavailable" }
  | { status: "error"; message: string };

/** PATCH /me/profile (13 §1.3) の入力。 */
export type ProfileUpdateInput = {
  displayName: string;
  description: string;
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
  /**
   * GET /me の取得状態。ok のときだけ data (マップ済みの Profile) を描画し、
   * loading / error / unavailable は画面を空にせずそれぞれの状態を出す。
   */
  state: ResourceState<Profile>;
  /** GET /memories (13 §2) は後続 todo。今は [] で空表示にする。 */
  memories: Memory[];
  /** error 状態の再取得。useResource の reload を渡す。 */
  onRetry?: () => void;
  /** ステータスの保存 (PUT /me/status, 13 §1)。未指定なら編集ボタンを無効にする。 */
  onSaveStatus?: (status: string) => Promise<StatusSaveResult>;
  /** 名前・自己紹介の保存 (PATCH /me/profile, 13 §1.3)。 */
  onSaveProfile?: (input: ProfileUpdateInput) => Promise<ProfileActionResult>;
  /** アイコンの変更 (PUT /me/avatar, multipart)。 */
  onChangeAvatar?: (file: File) => Promise<ProfileActionResult>;
  /** アイコンの削除 (DELETE /me/avatar)。 */
  onRemoveAvatar?: () => Promise<ProfileActionResult>;
  /** バナーの変更 (PUT /me/banner, multipart)。 */
  onChangeBanner?: (file: File) => Promise<ProfileActionResult>;
  /** バナーの削除 (DELETE /me/banner)。 */
  onRemoveBanner?: () => Promise<ProfileActionResult>;
};

export function ProfileScreen({
  state,
  memories,
  onRetry,
  onSaveStatus,
  onSaveProfile,
  onChangeAvatar,
  onRemoveAvatar,
  onChangeBanner,
  onRemoveBanner,
}: ProfileScreenProps) {
  return (
    <AppShell tab="profile">
      <div className={styles.content}>
        {state.status === "ok" ? (
          <ProfileBody
            me={state.data}
            memories={memories}
            onSaveStatus={onSaveStatus}
            onSaveProfile={onSaveProfile}
            onChangeAvatar={onChangeAvatar}
            onRemoveAvatar={onRemoveAvatar}
            onChangeBanner={onChangeBanner}
            onRemoveBanner={onRemoveBanner}
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
  memories: Memory[];
  onSaveStatus?: (status: string) => Promise<StatusSaveResult>;
  onSaveProfile?: (input: ProfileUpdateInput) => Promise<ProfileActionResult>;
  onChangeAvatar?: (file: File) => Promise<ProfileActionResult>;
  onRemoveAvatar?: () => Promise<ProfileActionResult>;
  onChangeBanner?: (file: File) => Promise<ProfileActionResult>;
  onRemoveBanner?: () => Promise<ProfileActionResult>;
};

function ProfileBody({
  me,
  memories,
  onSaveStatus,
  onSaveProfile,
  onChangeAvatar,
  onRemoveAvatar,
  onChangeBanner,
  onRemoveBanner,
}: ProfileBodyProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  // server の status を key にした楽観値。server 値が変わると表示も server に戻る。
  const [override, setOverride] = useState<{ base: string; value: string }>({
    base: "",
    value: "",
  });

  const [profileEditing, setProfileEditing] = useState(false);
  const [nameDraft, setNameDraft] = useState("");
  const [bioDraft, setBioDraft] = useState("");
  const [profileSaving, setProfileSaving] = useState(false);
  const [profileError, setProfileError] = useState<string | null>(null);
  const [profileNotice, setProfileNotice] = useState<string | null>(null);

  const [imageBusy, setImageBusy] = useState<string | null>(null);
  const [imageError, setImageError] = useState<string | null>(null);
  const [imageNotice, setImageNotice] = useState<string | null>(null);

  const [crop, setCrop] = useState<{ kind: ImageCropKind; file: File } | null>(
    null,
  );

  const avatarInputRef = useRef<HTMLInputElement>(null);
  const bannerInputRef = useRef<HTMLInputElement>(null);

  const serverStatus = me.status ?? "";
  const status = override.base === serverStatus ? override.value : serverStatus;

  const nameCount = graphemeCount(nameDraft);
  const bioCount = graphemeCount(bioDraft);
  const nameOver = nameCount > MAX_DISPLAY_NAME_GRAPHEMES;
  const bioOver = bioCount > MAX_PROFILE_DESCRIPTION_GRAPHEMES;

  const startEdit = () => {
    setDraft(status);
    setSaveError(null);
    setEditing(true);
    setProfileEditing(false);
  };

  const cancelEdit = () => {
    setEditing(false);
    setSaveError(null);
  };

  const save = async () => {
    if (!onSaveStatus || saving) {
      return;
    }
    setSaving(true);
    setSaveError(null);
    const result = await onSaveStatus(draft);
    setSaving(false);
    if (result.status === "ok") {
      setOverride({ base: serverStatus, value: result.value });
      setEditing(false);
      return;
    }
    setSaveError(
      result.status === "unavailable"
        ? "ステータスはまだ更新できません。"
        : result.message,
    );
  };

  const startProfileEdit = () => {
    setNameDraft(me.name);
    setBioDraft(me.bio);
    setProfileError(null);
    setProfileNotice(null);
    setProfileEditing(true);
    setEditing(false);
  };

  const cancelProfileEdit = () => {
    setProfileEditing(false);
    setProfileError(null);
  };

  const saveProfile = async () => {
    if (!onSaveProfile || profileSaving) {
      return;
    }
    if (nameOver) {
      setProfileError("名前は64文字までです");
      return;
    }
    if (bioOver) {
      setProfileError("自己紹介は256文字までです");
      return;
    }
    setProfileSaving(true);
    setProfileError(null);
    setProfileNotice(null);
    const result = await onSaveProfile({
      displayName: nameDraft.trim(),
      description: bioDraft.trim(),
    });
    setProfileSaving(false);
    if (result.status === "ok") {
      setProfileEditing(false);
      setProfileNotice("プロフィールを更新しました");
      return;
    }
    setProfileError(
      result.status === "unavailable"
        ? "プロフィールはまだ更新できません。"
        : result.message,
    );
  };

  const runImageAction = async (
    key: string,
    action: () => Promise<ProfileActionResult>,
    okMessage: string,
  ) => {
    if (imageBusy) {
      return;
    }
    setImageBusy(key);
    setImageError(null);
    setImageNotice(null);
    const result = await action();
    setImageBusy(null);
    if (result.status === "ok") {
      setImageNotice(okMessage);
      return;
    }
    setImageError(
      result.status === "unavailable"
        ? "この画像はまだ変更できません。"
        : result.message,
    );
  };

  // 選んだ画像はすぐ送らず、トリミング画面で位置と大きさを決めてから送る。
  const onFileSelected = (
    event: ChangeEvent<HTMLInputElement>,
    kind: ImageCropKind,
    action: ((file: File) => Promise<ProfileActionResult>) | undefined,
  ) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file || !action) {
      return;
    }
    const check = validateMediaFile(file);
    if (!check.ok) {
      setImageError(check.message);
      return;
    }
    setImageError(null);
    setCrop({ kind, file });
  };

  const confirmCrop = async (file: File) => {
    if (!crop) {
      return;
    }
    const isAvatar = crop.kind === "avatar";
    const action = isAvatar ? onChangeAvatar : onChangeBanner;
    if (!action) {
      return;
    }
    await runImageAction(
      "upload",
      () => action(file),
      isAvatar ? "アイコンを更新しました" : "バナーを更新しました",
    );
    setCrop(null);
  };

  const pickAvatar = () => avatarInputRef.current?.click();
  const pickBanner = () => bannerInputRef.current?.click();

  const removeAvatar = () => {
    if (!onRemoveAvatar) {
      return;
    }
    return runImageAction("remove-avatar", onRemoveAvatar, "アイコンを削除しました");
  };

  const removeBanner = () => {
    if (!onRemoveBanner) {
      return;
    }
    return runImageAction("remove-banner", onRemoveBanner, "バナーを削除しました");
  };

  return (
    <>
      <input
        ref={avatarInputRef}
        type="file"
        accept={ACCEPTED_MEDIA_TYPES.join(",")}
        hidden
        data-testid="profile-avatar-input"
        onChange={(event) =>
          onFileSelected(event, "avatar", onChangeAvatar)
        }
      />
      <input
        ref={bannerInputRef}
        type="file"
        accept={ACCEPTED_MEDIA_TYPES.join(",")}
        hidden
        data-testid="profile-banner-input"
        onChange={(event) =>
          onFileSelected(event, "banner", onChangeBanner)
        }
      />

      {crop && (
        <ImageCropDialog
          kind={crop.kind}
          file={crop.file}
          busy={imageBusy !== null}
          onCancel={() => setCrop(null)}
          onConfirm={(file) => void confirmCrop(file)}
        />
      )}

      <div className={styles.bannerWrap}>
        <div className={styles.banner} aria-hidden>
          {me.bannerUrl ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img className={styles.bannerImage} src={me.bannerUrl} alt="" />
          ) : (
            <>
              <span className={`${styles.cloud} ${styles.cloudA}`} />
              <span className={`${styles.cloud} ${styles.cloudB}`} />
              <span className={`${styles.cloud} ${styles.cloudC}`} />
            </>
          )}
        </div>
        <div className={styles.bannerEdit}>
          <IconButton
            icon="edit"
            label="バナーを編集"
            variant="surface"
            onClick={onChangeBanner ? pickBanner : undefined}
            disabled={!onChangeBanner || imageBusy !== null}
          />
        </div>
        <span className={styles.avatar}>
          <Avatar user={me} size={80} />
        </span>
        <div className={styles.editButton}>
          <Button
            variant="secondary"
            size="sm"
            icon="edit"
            onClick={onSaveStatus ? startEdit : undefined}
            disabled={!onSaveStatus || editing}
          >
            編集
          </Button>
        </div>
      </div>

      <div className={styles.nameRow}>
        <h1 className={styles.name}>{me.name}</h1>
        {status && !editing && (
          <span className={styles.status}>
            <span className={styles.onlineDot} aria-hidden />
            {status}
          </span>
        )}
      </div>

      {editing && (
        <div className={styles.statusEditor}>
          <label className={styles.statusLabel} htmlFor="profile-status">
            ステータス
          </label>
          <input
            id="profile-status"
            className={styles.statusInput}
            value={draft}
            maxLength={40}
            disabled={saving}
            onChange={(event) => setDraft(event.target.value)}
          />
          <div className={styles.statusActions}>
            <Button variant="secondary" size="sm" onClick={cancelEdit} disabled={saving}>
              キャンセル
            </Button>
            <Button variant="primary" size="sm" onClick={() => void save()} disabled={saving}>
              {saving ? "保存しています…" : "保存"}
            </Button>
          </div>
          {saveError && (
            <div role="alert">
              <Notice tone="warning" icon="error">
                {saveError}
              </Notice>
            </div>
          )}
        </div>
      )}

      <p className={styles.bio}>{me.bio}</p>

      {profileNotice && (
        <div role="status">
          <Notice tone="info" icon="check">
            {profileNotice}
          </Notice>
        </div>
      )}
      {imageNotice && (
        <div role="status">
          <Notice tone="info" icon="check">
            {imageNotice}
          </Notice>
        </div>
      )}
      {imageError && (
        <div role="alert">
          <Notice tone="warning" icon="error">
            {imageError}
          </Notice>
        </div>
      )}

      {profileEditing ? (
        <div className={styles.profileEditor}>
          <label className={styles.fieldLabel} htmlFor="profile-display-name">
            名前
          </label>
          <input
            id="profile-display-name"
            className={styles.fieldInput}
            value={nameDraft}
            disabled={profileSaving}
            onChange={(event) => setNameDraft(event.target.value)}
          />
          <span className={nameOver ? styles.counterOver : styles.counter}>
            {nameCount}/{MAX_DISPLAY_NAME_GRAPHEMES}
          </span>

          <label className={styles.fieldLabel} htmlFor="profile-description">
            自己紹介
          </label>
          <textarea
            id="profile-description"
            className={styles.fieldTextarea}
            value={bioDraft}
            disabled={profileSaving}
            onChange={(event) => setBioDraft(event.target.value)}
          />
          <span className={bioOver ? styles.counterOver : styles.counter}>
            {bioCount}/{MAX_PROFILE_DESCRIPTION_GRAPHEMES}
          </span>

          <div className={styles.imageRow}>
            <span className={styles.imageLabel}>アイコン</span>
            <div className={styles.imageActions}>
              <Button
                variant="secondary"
                size="sm"
                onClick={pickAvatar}
                disabled={!onChangeAvatar || imageBusy !== null}
              >
                アイコンを変更
              </Button>
              {me.avatarUrl && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => void removeAvatar()}
                  disabled={!onRemoveAvatar || imageBusy !== null}
                >
                  アイコンを削除
                </Button>
              )}
            </div>
          </div>

          <div className={styles.imageRow}>
            <span className={styles.imageLabel}>バナー</span>
            <div className={styles.imageActions}>
              <Button
                variant="secondary"
                size="sm"
                onClick={pickBanner}
                disabled={!onChangeBanner || imageBusy !== null}
              >
                バナーを変更
              </Button>
              {me.bannerUrl && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => void removeBanner()}
                  disabled={!onRemoveBanner || imageBusy !== null}
                >
                  バナーを削除
                </Button>
              )}
            </div>
          </div>

          {imageBusy !== null && (
            <p className={styles.uploading} role="status">
              送信しています…
            </p>
          )}

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
              </Notice>
            </div>
          )}
        </div>
      ) : (
        <div className={styles.profileEditRow}>
          <Button
            variant="secondary"
            size="sm"
            icon="edit"
            onClick={onSaveProfile ? startProfileEdit : undefined}
            disabled={!onSaveProfile}
          >
            プロフィールを編集
          </Button>
        </div>
      )}

      <StatTriple
        items={[
          { value: me.counts.encounters, label: "すれ違い" },
          { value: me.counts.friends, label: "友達" },
          { value: me.counts.parties, label: "パーティ" },
        ]}
      />

      <section className={styles.section}>
        <SectionHeader
          title="メモリー"
          action={{ label: "すべて見る ›", href: "/memory/" }}
        />
        <div className={styles.memories}>
          {memories.length > 0 ? (
            memories.map((memory) => (
              <MemoryThumbCard key={memory.spaceId} memory={memory} />
            ))
          ) : (
            <p className={styles.memoryEmpty}>まだメモリーはありません</p>
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
    </>
  );
}

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
