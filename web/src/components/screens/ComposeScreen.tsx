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
import { Toggle } from "@/components/ui/Toggle";
import styles from "./ComposeScreen.module.css";

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

function hintFor(targets: PostTarget[]): string {
  if (targets.length === 0) {
    return "送信先を 1〜5 件選んでください";
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
  const [alsoPublic, setAlsoPublic] = useState(false);
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
    return targets.length > 0 ? [targets[0].spaceId] : [];
  }, [override, targets, initialSpaceIds]);

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
    validation.ok && targets.length > 0 && !submitting && !uploading && !consentRequired;

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

  const selectorLabel =
    selectedTargets.length === 0
      ? "送信先を選ぶ"
      : selectedTargets.length === 1
        ? selectedTargets[0].name
        : `${selectedTargets[0].name} 他${selectedTargets.length - 1}件`;
  const selectorIcon =
    selectedTargets.length === 1 ? iconFor(selectedTargets[0]) : "hub";

  const remaining = HINT_LENGTH - body.length;

  return (
    <div className={styles.screen}>
      <header className={styles.header}>
        <button type="button" className={styles.cancel} onClick={cancel}>
          キャンセル
        </button>
        <button
          type="button"
          className={styles.submit}
          onClick={submit}
          disabled={!canPost}
        >
          {submitting ? "送信中…" : "投稿する"}
        </button>
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

        {targetsState.status === "unavailable" && (
          <Card padding="md" className={styles.stateCard}>
            投稿先はまだ利用できません
          </Card>
        )}

        {targetsState.status === "empty" && (
          <Card padding="md" className={styles.stateCard}>
            投稿できる送信先がありません
          </Card>
        )}

        {targetsState.status === "ok" && targets.length === 0 && (
          <Card padding="md" className={styles.stateCard}>
            投稿できる送信先がありません
          </Card>
        )}

        {targetsState.status === "ok" && targets.length > 0 && (
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
                    {targets.map((item) => (
                      <button
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
                      </button>
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
                <button
                  type="button"
                  className={styles.remove}
                  aria-label="画像を削除"
                  onClick={() => onRemoveAttachment?.(attachment.key)}
                >
                  <Icon name="close" size={16} />
                </button>
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
              <p className={styles.hint}>{hintFor(selectedTargets)}</p>
            </div>

            <div className={styles.optionRow}>
              <div className={styles.optionText}>
                <span className={styles.optionTitle}>公開投稿も作成</span>
                <span className={styles.optionDesc}>
                  プロフィールの公開フィードにも同じ本文を投稿します
                </span>
              </div>
              <Toggle
                checked={alsoPublic}
                onChange={setAlsoPublic}
                label="公開投稿も作成"
              />
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
