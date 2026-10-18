"use client";

import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type ChangeEvent } from "react";
import type { GroupVisibility, User } from "@/domain/types";
import { validateMediaFile } from "@/components/feature/compose";
import {
  InviteePicker,
  VisibilityOption,
} from "@/components/feature/group-create";
import { ImageCropDialog } from "@/components/feature/imageCrop";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Notice } from "@/components/ui/Notice";
import { Slider } from "@/components/ui/Slider";
import { Toggle } from "@/components/ui/Toggle";
import styles from "./CreateGroupScreen.module.css";
import { Pressable } from "@/components/ui/Pressable";

const MAX_NAME = 40;
const MAX_DESCRIPTION = 500;
const MIN_RADIUS_M = 5;
const MAX_RADIUS_M = 2000;
const DEFAULT_RADIUS_M = 40;
/** 半径スライダーの分解能。位置は対数で半径に写す (5m 付近は細かく、2km 付近は粗く)。 */
const RADIUS_SLIDER_MAX = 1000;

/** 半径を区切りのいい値に丸める。20m 未満は 1m、100m 未満は 5m、1km 未満は 10m、それ以上は 50m 刻み。 */
function roundRadius(m: number): number {
  const step = m < 20 ? 1 : m < 100 ? 5 : m < 1_000 ? 10 : 50;
  return Math.min(MAX_RADIUS_M, Math.max(MIN_RADIUS_M, Math.round(m / step) * step));
}

/** スライダーの位置 (0〜RADIUS_SLIDER_MAX) → 半径 (m)。 */
function radiusFromSlider(position: number): number {
  const ratio = position / RADIUS_SLIDER_MAX;
  return roundRadius(MIN_RADIUS_M * (MAX_RADIUS_M / MIN_RADIUS_M) ** ratio);
}

/** 半径 (m) → スライダーの位置。 */
function sliderFromRadius(m: number): number {
  return Math.round(
    (Math.log(m / MIN_RADIUS_M) / Math.log(MAX_RADIUS_M / MIN_RADIUS_M)) * RADIUS_SLIDER_MAX,
  );
}

function formatRadius(m: number): string {
  return m >= 1_000 ? `${(m / 1_000).toFixed(m % 1_000 === 0 ? 0 : 2).replace(/0$/, "")}km` : `${m}m`;
}
const DEFAULT_QUORUM_PERCENT = 60;
const QUORUM_STEP_PERCENT = 10;

/** POST /groups に送る前の画面の入力。geofence の中心はページ側が現在地から足す。 */
export type CreateGroupDraft = {
  name: string;
  description: string;
  visibility: GroupVisibility;
  persistent: boolean;
  radiusM: number;
  inviteQuorum: number;
  force: boolean;
};

/** 409 conflict の details.similar (09 §2)。 */
export type SimilarGroup = { id: string; name: string };

/** onSubmit が返す、画面が出し分ける作成結果。 */
export type CreateGroupResult =
  | { status: "ok"; groupId: string }
  | { status: "conflict"; similar: SimilarGroup[] }
  | { status: "location_stale" }
  | { status: "unavailable" }
  | { status: "error"; message: string };

export type CreateGroupScreenProps = {
  candidates: User[];
  /** 現在地 (useLocation の fix)。null の間は作成できない。 */
  location: { lat: number; lon: number } | null;
  /** useLocation のエラー。位置が取れない理由を表示する。 */
  locationError?: string | null;
  onSubmit: (draft: CreateGroupDraft) => Promise<CreateGroupResult>;
  /** アイコン画像を POST /media に上げ、mediaId を返す (10 §4.4)。 */
  onUploadIcon?: (file: File) => Promise<string | null>;
  onCancel?: () => void;
};

type IconState = "idle" | "uploading" | "ready" | "error";

export function CreateGroupScreen({
  candidates,
  location,
  locationError,
  onSubmit,
  onUploadIcon,
  onCancel,
}: CreateGroupScreenProps) {
  const router = useRouter();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<GroupVisibility>("open");
  const [persistent, setPersistent] = useState(false);
  const [radiusM, setRadiusM] = useState(DEFAULT_RADIUS_M);
  const [quorumPercent, setQuorumPercent] = useState(DEFAULT_QUORUM_PERCENT);
  const [inviteeIds, setInviteeIds] = useState<string[]>([]);
  const [picking, setPicking] = useState(false);
  const [showDetails, setShowDetails] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [similar, setSimilar] = useState<SimilarGroup[]>([]);
  const [stale, setStale] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [iconState, setIconState] = useState<IconState>("idle");
  const [iconError, setIconError] = useState<string | null>(null);
  const [cropFile, setCropFile] = useState<File | null>(null);
  const [iconPreview, setIconPreview] = useState<string | null>(null);
  const iconInput = useRef<HTMLInputElement>(null);

  // プレビューの object URL は差し替え・離脱のときに解放する
  useEffect(() => {
    if (!iconPreview) return;
    return () => URL.revokeObjectURL(iconPreview);
  }, [iconPreview]);

  // 承諾は「招待した人のうち何割か」で選び、送るときに人数 (inviteQuorum) へ直す
  const inviteeCount = inviteeIds.length;
  const quorum = Math.max(1, Math.ceil((inviteeCount * quorumPercent) / 100));
  const invitees = candidates.filter((candidate) => inviteeIds.includes(candidate.did));
  const others = candidates.filter((candidate) => !inviteeIds.includes(candidate.did));
  const canSubmit = name.trim().length > 0 && location !== null && !submitting;

  const toggleInvitee = (id: string) => {
    setInviteeIds((prev) =>
      prev.includes(id) ? prev.filter((item) => item !== id) : [...prev, id],
    );
  };

  const submit = async (force: boolean) => {
    if (!canSubmit) {
      return;
    }
    setSubmitting(true);
    setSimilar([]);
    setStale(false);
    setErrorMessage(null);
    const result = await onSubmit({
      name: name.trim(),
      description: description.trim(),
      visibility,
      persistent,
      radiusM,
      // inviteQuorum は「承認が必要な人数」。open では 1 で作る (09 §6)。
      inviteQuorum: visibility === "closed" ? quorum : 1,
      force,
    });
    setSubmitting(false);
    switch (result.status) {
      case "ok":
        router.replace(`/group/?id=${encodeURIComponent(result.groupId)}`);
        break;
      case "conflict":
        setSimilar(result.similar);
        break;
      case "location_stale":
        setStale(true);
        break;
      case "unavailable":
        setErrorMessage("グループ作成はまだ利用できません。");
        break;
      case "error":
        setErrorMessage(result.message);
        break;
    }
  };

  const cancel = () => {
    onCancel?.();
    router.back();
  };

  const pickIcon = () => {
    if (iconState === "uploading") {
      return;
    }
    iconInput.current?.click();
  };

  // プロフィールのアイコンと同じく、選んだ画像はトリミングしてから送り、丸いプレビューに出す
  const changeIcon = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file || !onUploadIcon) {
      return;
    }
    const check = validateMediaFile(file);
    if (!check.ok) {
      setIconError(check.message);
      setIconState("error");
      return;
    }
    setIconError(null);
    setCropFile(file);
  };

  const confirmIcon = async (file: File) => {
    if (!onUploadIcon) {
      return;
    }
    setIconState("uploading");
    const mediaId = await onUploadIcon(file);
    setCropFile(null);
    if (mediaId) {
      setIconPreview(URL.createObjectURL(file));
      setIconState("ready");
    } else {
      setIconState("error");
    }
  };

  // アイコンの mediaId は POST /groups の body に項目が無いため、今は保持だけして送らない。
  // グループのアバター項目が backend にできたら、この値を body に足す。

  return (
    <div className={styles.screen}>
      <header className={styles.header}>
        <Pressable type="button" className={styles.cancel} onClick={cancel}>
          キャンセル
        </Pressable>
        <span className={styles.heading}>グループを作る</span>
        <Pressable
          type="button"
          className={styles.submit}
          onClick={() => void submit(false)}
          disabled={!canSubmit}
        >
          {submitting ? "作成中…" : "作成"}
        </Pressable>
      </header>

      <div className={styles.nameSection}>
        <Pressable
          type="button"
          className={styles.iconPicker}
          data-preview={iconPreview ? "" : undefined}
          aria-label={iconPreview ? "グループアイコンを変える" : "グループアイコンを選ぶ"}
          onClick={pickIcon}
        >
          {iconPreview ? (
            <>
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img className={styles.iconPreview} src={iconPreview} alt="" />
              <span className={styles.iconBadge} aria-hidden>
                <Icon name="photo_camera" size={14} />
              </span>
            </>
          ) : (
            <Icon name="add_a_photo" size={28} />
          )}
        </Pressable>
        <input
          ref={iconInput}
          type="file"
          accept="image/jpeg,image/png,image/webp"
          className={styles.fileInput}
          onChange={changeIcon}
        />
        {cropFile && (
          <ImageCropDialog
            kind="avatar"
            file={cropFile}
            busy={iconState === "uploading"}
            onCancel={() => setCropFile(null)}
            onConfirm={(file) => void confirmIcon(file)}
          />
        )}
        <div className={styles.nameField}>
          <label className={styles.fieldLabel} htmlFor="group-name">
            グループ名
          </label>
          <input
            id="group-name"
            className={styles.input}
            value={name}
            maxLength={MAX_NAME}
            onChange={(event) => setName(event.target.value)}
          />
          <span className={styles.counter}>
            {name.length}/{MAX_NAME}
          </span>
        </div>
      </div>

      <div className={styles.band} />

      <section className={styles.section}>
        <h2 className={styles.sectionLabel}>公開範囲</h2>
        <div role="radiogroup" aria-label="公開範囲">
          <VisibilityOption
            icon="public"
            title="オープン"
            description="エリアにいる人がリストから自由に参加できます"
            selected={visibility === "open"}
            tone="notice"
            onSelect={() => setVisibility("open")}
          />
          <VisibilityOption
            icon="lock"
            title="クローズド（招待制）"
            description="招待された人が承諾するとグループができます"
            selected={visibility === "closed"}
            tone="primary"
            onSelect={() => setVisibility("closed")}
          />
        </div>
      </section>

      {visibility === "closed" && (
        <>
          <div className={styles.band} />
          <section className={styles.section}>
            <div>
              <h2 className={styles.sectionLabel}>承諾が必要な割合</h2>
              <p className={styles.sectionDesc}>
                招待した人のうち、この割合が承諾するとグループが作られます
              </p>
            </div>
            <div className={styles.ratioRow}>
              <span className={styles.ratioValue}>{quorumPercent}%</span>
              <span className={styles.ratioNote}>
                {inviteeCount > 0
                  ? `${inviteeCount}人中 ${quorum}人が承諾すれば作成`
                  : "招待する人を選んでください"}
              </span>
            </div>
            <Slider
              min={0}
              max={100}
              step={QUORUM_STEP_PERCENT}
              value={quorumPercent}
              tone="primary"
              label="承諾が必要な割合"
              marks={["1人", "50%", "全員"]}
              onChange={setQuorumPercent}
            />
          </section>
        </>
      )}

      <div className={styles.band} />

      <section className={styles.section}>
        <div className={styles.sectionHead}>
          <h2 className={styles.sectionLabel}>招待する人</h2>
          <span className={styles.count}>{inviteeCount}人</span>
        </div>
        <InviteePicker
          candidates={invitees}
          selectedIds={inviteeIds}
          onToggle={toggleInvitee}
          onAdd={() => setPicking((prev) => !prev)}
        />
        {picking &&
          (others.length > 0 ? (
            <div className={styles.picker}>
              <p className={styles.sectionDesc}>フレンドから選ぶ</p>
              <InviteePicker
                candidates={others}
                selectedIds={inviteeIds}
                onToggle={toggleInvitee}
              />
            </div>
          ) : (
            <p className={styles.sectionDesc}>招待できるフレンドはもういません</p>
          ))}
      </section>

      <div className={styles.band} />

      <section className={styles.section}>
        <Pressable
          type="button"
          className={styles.detailsToggle}
          aria-expanded={showDetails}
          onClick={() => setShowDetails((prev) => !prev)}
        >
          <span className={styles.sectionLabel}>詳細設定</span>
          <Icon name={showDetails ? "expand_less" : "expand_more"} size={20} />
        </Pressable>
        {showDetails && (
          <div className={styles.details}>
            <div className={styles.field}>
              <label className={styles.sectionTitle} htmlFor="group-description">
                説明
              </label>
              <textarea
                id="group-description"
                className={styles.textarea}
                value={description}
                maxLength={MAX_DESCRIPTION}
                rows={3}
                onChange={(event) => setDescription(event.target.value)}
              />
              <span className={styles.counter}>
                {description.length}/{MAX_DESCRIPTION}
              </span>
            </div>

            <div className={styles.field}>
              <h3 className={styles.sectionTitle}>エリアの広さ</h3>
              <p className={styles.sectionDesc}>
                今いる場所を中心にした円の半径です。この中にいる人が参加できます
              </p>
              <span className={styles.radiusValue}>{formatRadius(radiusM)}</span>
              <Slider
                min={0}
                max={RADIUS_SLIDER_MAX}
                step={1}
                value={sliderFromRadius(radiusM)}
                tone="primary"
                label="エリアの半径"
                marks={["5m", "100m", "2km"]}
                onChange={(position) => setRadiusM(radiusFromSlider(position))}
              />
            </div>

            <div className={styles.toggleRow}>
              <div className={styles.toggleText}>
                <h3 className={styles.sectionTitle}>参加後もエリア外で使う</h3>
                <p className={styles.sectionDesc}>
                  エリアを離れても、閲覧と投稿ができるようになります
                </p>
              </div>
              <Toggle
                checked={persistent}
                onChange={setPersistent}
                label="参加後もエリア外で使う"
              />
            </div>
          </div>
        )}
      </section>

      {similar.length > 0 && (
        <section className={styles.conflict}>
          <p className={styles.conflictTitle}>
            同じ場所に似たグループがすでにあります
          </p>
          <ul className={styles.conflictList}>
            {similar.map((group) => (
              <li key={group.id} className={styles.conflictItem}>
                {group.name}
              </li>
            ))}
          </ul>
          <Button
            variant="secondary"
            icon="add"
            disabled={submitting}
            onClick={() => void submit(true)}
          >
            それでも作成する
          </Button>
        </section>
      )}

      {stale && (
        <div className={styles.notice}>
          <Notice tone="warning" icon="location_off">
            位置情報が古いため作成できませんでした。少し待ってからもう一度お試しください。
          </Notice>
        </div>
      )}

      {errorMessage && (
        <div className={styles.notice}>
          <Notice tone="warning" icon="error">
            {errorMessage}
          </Notice>
        </div>
      )}

      {iconState === "error" && (
        <div className={styles.notice}>
          <Notice tone="warning" icon="error">
            {iconError ?? "アイコンをアップロードできませんでした。"}
          </Notice>
        </div>
      )}

      <p className={styles.footer}>
        <Icon
          name={locationError ? "location_off" : location ? "location_on" : "location_searching"}
          size={16}
        />
        {locationError
          ? `位置情報を取得できませんでした（${locationError}）`
          : location
            ? `グループは今いるエリア（半径${radiusM}m）に作られます`
            : "現在地を取得しています…"}
      </p>
    </div>
  );
}
