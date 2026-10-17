"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import type { PostTarget, User } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import {
  CharCounterRing,
  TargetSelector,
  validateComposeInput,
  type ComposeAttachment,
  type ComposeSubmitInput,
} from "@/components/feature/compose";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Chip } from "@/components/ui/Chip";
import { Icon } from "@/components/ui/Icon";
import { IconButton } from "@/components/ui/IconButton";
import { ImagePlaceholder } from "@/components/ui/ImagePlaceholder";
import { Notice } from "@/components/ui/Notice";
import styles from "./ComposeScreen.module.css";
import { Pressable } from "@/components/ui/Pressable";

/** 文字数の目安。サーバ上限ではない (backend は 2000 ルーン、10 §3)。 */
const HINT_LENGTH = 140;
/** 送信先の上限 (10 §4.1)。 */
const MAX_TARGETS = 5;

/** 送信失敗の表示種別。page.tsx が ApiError を分類して渡す。 */
export type ComposeSubmitError =
  | { kind: "consent" }
  | { kind: "unavailable" }
  | { kind: "invalid"; message: string }
  | { kind: "error"; message: string }
  /** instance 投稿は成功したが、同時作成した公開投稿だけ失敗した (10 §4.1)。 */
  | { kind: "public"; message: string };

export type ComposeScreenProps = {
  /** GET /me の著者 (アバター用)。取得できないときは省略。 */
  me?: User;
  /** GET /instances の取得状態 (13 §2)。 */
  targetsState: ResourceState<PostTarget[]>;
  /** URL (?target=type:spaceId) から復元する初期送信先。 */
  initialSpaceIds?: string[];
  /** URL (?target=public) から開いたとき、公開だけを初期の送信先にする。 */
  initialPublic?: boolean;
  /** 添付。未アップロードのものは id を持たない (todo 15)。 */
  attachments?: ComposeAttachment[];
  /** POST /posts の送信中か。 */
  submitting?: boolean;
  /** 添付 (POST /media) のアップロード中か (todo 15)。送信を止める。 */
  uploading?: boolean;
  /** 直前の送信失敗。 */
  error?: ComposeSubmitError | null;
  /** 実際の送信は page.tsx が行う (POST /posts)。 */
  onSubmit: (input: ComposeSubmitInput) => void;
  /** 添付ピッカーを開く (todo 15)。未指定なら添付ボタンを無効にする。 */
  onPickAttachment?: () => void;
  onRemoveAttachment?: (key: string) => void;
  /** 403 consent_required のときの同意導線 (todo 35)。 */
  onConsent?: () => void;
  /** 内容チェックへの同意がまだ済んでいない (PUT /me/consents)。true の間は投稿させない。 */
  consentRequired?: boolean;
  /** 送信先の再取得。 */
  targetsRetry?: () => void;
  onCancel?: () => void;
};

function hintFor(targets: PostTarget[], alsoPublic: boolean): string {
  if (targets.length === 0) {
    return alsoPublic
      ? "フレンドやすれ違った人のホームに公開で表示されます"
      : "送信先を選んでください";
  }
  if (alsoPublic) {
    return "選んだ Space のタイムラインと、公開のホームに表示されます";
  }
  if (targets.length > 1) {
    return "選んだ Space のタイムラインに表示されます";
  }
  const [target] = targets;
  return target.type === "group"
    ? `グループのエリアにいる ${target.memberCount}人 のタイムラインに表示されます`
    : "パーティのメンバーに表示されます";
}

function iconFor(target: PostTarget): string {
  return target.type === "group" ? "hub" : "celebration";
}

export function ComposeScreen({
  me,
  targetsState,
  initialSpaceIds,
  initialPublic = false,
  attachments = [],
  submitting = false,
  uploading = false,
  error = null,
  onSubmit,
  onPickAttachment,
  onRemoveAttachment,
  onConsent,
  consentRequired = false,
  targetsRetry,
  onCancel,
}: ComposeScreenProps) {
  const router = useRouter();
  const targets = useMemo(
    () => (targetsState.status === "ok" ? targetsState.data : []),
    [targetsState],
  );

  // 選択は override に持つ。未操作なら initial → 先頭 target の順に既定を導く
  // (targets は非同期に届くので、effect で setState せず描画時に決める)。
  const [override, setOverride] = useState<string[] | null>(null);
  const [body, setBody] = useState("");
  const [alsoPublic, setAlsoPublic] = useState(initialPublic);
  const [menuOpen, setMenuOpen] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const selectedIds = useMemo(() => {
    const exists = (id: string) => targets.some((t) => t.spaceId === id);
    if (override) {
      return override.filter(exists);
    }
    const initial = (initialSpaceIds ?? []).filter(exists);
    if (initial.length > 0) {
      return initial;
    }
    if (initialPublic) {
      return [];
    }
    return targets.length > 0 ? [targets[0].spaceId] : [];
  }, [override, targets, initialSpaceIds, initialPublic]);

  const selectedTargets = useMemo(
    () => selectedIds
      .map((id) => targets.find((t) => t.spaceId === id))
      .filter((t): t is PostTarget => t !== undefined),
    [selectedIds, targets],
  );

  const input: ComposeSubmitInput = {
    spaceIds: selectedIds,
    text: body,
    format: "plain",
    attachments,
    alsoPublic,
  };
  const validation = validateComposeInput(input);

  useEffect(() => {
    const node = textareaRef.current;
    if (node) {
      node.style.height = "auto";
      node.style.height = `${node.scrollHeight}px`;
    }
  }, [body]);

  const toggleTarget = (spaceId: string) => {
    setOverride((prev) => {
      const base = prev ?? selectedIds;
      if (base.includes(spaceId)) {
        return base.filter((id) => id !== spaceId);
      }
      return base.length >= MAX_TARGETS ? base : [...base, spaceId];
    });
  };

  const showReason =
    !validation.ok &&
    (body.length > 0 || attachments.length > 0 || selectedIds.length === 0);

  const canPost =
    validation.ok && !submitting && !uploading && !consentRequired;

  const submit = () => {
    if (!canPost) {
      return;
    }
    onSubmit(input);
  };

  const cancel = () => {
    if (onCancel) {
      onCancel();
    } else {
      router.back();
    }
  };

  // 公開は送信先の 1 つとして数える (表示上だけ。POST の spaceIds には入らない)。
  const selectedNames = [
    ...(alsoPublic ? ["公開"] : []),
    ...selectedTargets.map((target) => target.name),
  ];
  const selectorLabel =
    selectedNames.length === 0
      ? "送信先を選ぶ"
      : selectedNames.length === 1
        ? selectedNames[0]
        : `${selectedNames[0]} 他${selectedNames.length - 1}件`;
  const selectorIcon =
    selectedNames.length !== 1
      ? "hub"
      : alsoPublic
        ? "public"
        : iconFor(selectedTargets[0]);

  const remaining = HINT_LENGTH - body.length;

  return (
    <div className={styles.screen}>
      <header className={styles.header}>
        <Pressable type="button" className={styles.cancel} onClick={cancel}>
          キャンセル
        </Pressable>
        <Pressable
          type="button"
          className={styles.submit}
          onClick={submit}
          disabled={!canPost}
        >
          {submitting ? "送信中…" : "投稿する"}
        </Pressable>
      </header>

      <div className={styles.body}>
        {targetsState.status === "loading" && (
          <Card padding="md" className={styles.stateCard}>
            読み込み中…
          </Card>
        )}

        {targetsState.status === "error" && (
          <Card padding="md" className={styles.stateStack}>
            <p className={styles.stateText}>送信先を読み込めませんでした</p>
            {targetsRetry && (
              <Button variant="secondary" icon="refresh" onClick={targetsRetry}>
                再読み込み
              </Button>
            )}
          </Card>
        )}

        {/* Space が無い・取れないときも、公開だけの投稿はできる。 */}
        {targetsState.status !== "loading" && (
          <>
            <div className={styles.targetRow}>
              {me && <Avatar user={me} size={40} />}
              <div className={styles.targetWrap}>
                <TargetSelector
                  label={selectorLabel}
                  icon={selectorIcon}
                  onClick={() => setMenuOpen((open) => !open)}
                />
                {menuOpen && (
                  <div className={styles.menu} role="menu">
                    <Pressable
                      type="button"
                      role="menuitemcheckbox"
                      aria-checked={alsoPublic}
                      className={styles.menuItem}
                      onClick={() => setAlsoPublic((value) => !value)}
                    >
                      <Icon name="public" size={18} />
                      <span className={styles.menuName}>公開</span>
                      {alsoPublic && <Icon name="check" size={18} />}
                    </Pressable>
                    {targets.map((item) => (
                      <Pressable
                        key={item.spaceId}
                        type="button"
                        role="menuitemcheckbox"
                        aria-checked={selectedIds.includes(item.spaceId)}
                        className={styles.menuItem}
                        onClick={() => toggleTarget(item.spaceId)}
                      >
                        <Icon name={iconFor(item)} size={18} />
                        <span className={styles.menuName}>{item.name}</span>
                        {selectedIds.includes(item.spaceId) && (
                          <Icon name="check" size={18} />
                        )}
                      </Pressable>
                    ))}
                  </div>
                )}
              </div>
            </div>

            <textarea
              ref={textareaRef}
              className={styles.textarea}
              value={body}
              placeholder="いまどうしてる？"
              aria-label="投稿本文"
              rows={1}
              onChange={(event) => setBody(event.target.value)}
            />

            {attachments.map((attachment) => (
              <div key={attachment.key} className={styles.attachment}>
                <ImagePlaceholder
                  ratio="1/1"
                  src={attachment.previewUrl}
                  alt={attachment.alt ?? ""}
                />
                <Pressable
                  type="button"
                  className={styles.remove}
                  aria-label="画像を削除"
                  onClick={() => onRemoveAttachment?.(attachment.key)}
                >
                  <Icon name="close" size={16} />
                </Pressable>
              </div>
            ))}

            {uploading && (
              <p className={styles.reason} role="status">
                画像をアップロード中…
              </p>
            )}

            <div className={styles.targets}>
              <span className={styles.caption}>送信先</span>
              <div className={styles.chipRow}>
                <Chip
                  label="公開"
                  icon="public"
                  selected={alsoPublic}
                  surface="surface"
                  onClick={() => setAlsoPublic((value) => !value)}
                />
                {targets.map((item) => (
                  <Chip
                    key={item.spaceId}
                    label={item.name}
                    icon={iconFor(item)}
                    selected={selectedIds.includes(item.spaceId)}
                    surface="surface"
                    onClick={() => toggleTarget(item.spaceId)}
                  />
                ))}
              </div>
              <p className={styles.hint}>{hintFor(selectedTargets, alsoPublic)}</p>
            </div>

            {consentRequired && (
              <div className={styles.errorBlock}>
                <Notice tone="warning" icon="verified_user">
                  投稿には内容チェックへの同意が必要です
                </Notice>
                {onConsent && (
                  <Button variant="secondary" onClick={onConsent}>
                    同意する
                  </Button>
                )}
              </div>
            )}

            {showReason && !validation.ok && (
              <p className={styles.reason}>{validation.message}</p>
            )}

            {error && (
              <div className={styles.errorBlock}>
                {error.kind === "consent" ? (
                  <>
                    <Notice tone="warning" icon="verified_user">
                      投稿には内容チェックへの同意が必要です
                    </Notice>
                    {onConsent && (
                      <Button variant="secondary" onClick={onConsent}>
                        同意する
                      </Button>
                    )}
                  </>
                ) : error.kind === "unavailable" ? (
                  <Notice tone="warning" icon="cloud_off">
                    Space への投稿はまだ利用できません
                  </Notice>
                ) : error.kind === "public" ? (
                  <Notice tone="info" icon="public">
                    {error.message}
                  </Notice>
                ) : (
                  <Notice tone="warning" icon="error">
                    {error.message}
                  </Notice>
                )}
              </div>
            )}
          </>
        )}
      </div>

      <footer className={styles.toolbar}>
        <div className={styles.tools}>
          <IconButton
            icon="image"
            label="画像を追加"
            onClick={onPickAttachment}
            disabled={!onPickAttachment}
          />
          <IconButton
            icon="photo_camera"
            label="カメラで撮影"
            onClick={onPickAttachment}
            disabled={!onPickAttachment}
          />
          <IconButton icon="location_on" label="位置を追加" disabled />
          <IconButton icon="checklist" label="アンケートを追加" disabled />
        </div>
        <div className={styles.counter}>
          <span className={remaining < 0 ? styles.remainingOver : styles.remaining}>
            {remaining >= 0 ? `あと ${remaining}` : `${-remaining} 字超過`}
          </span>
          <CharCounterRing value={body.length} max={HINT_LENGTH} />
        </div>
      </footer>

      <div className={styles.notice}>
        <Notice tone="info" icon="verified_user">
          投稿前に、誹謗中傷にならないかAIがチェックします
        </Notice>
      </div>
    </div>
  );
}
