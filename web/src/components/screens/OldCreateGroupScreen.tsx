"use client";

import { useRouter } from "next/navigation";
import { useRef, useState, type ChangeEvent } from "react";
import type { GroupVisibility, User } from "@/domain/types";
import {
  InviteePicker,
  VisibilityOption,
} from "@/components/feature/group-create";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Notice } from "@/components/ui/Notice";
import { Slider } from "@/components/ui/Slider";
import { Toggle } from "@/components/ui/Toggle";
import styles from "./OldCreateGroupScreen.module.css";
import { Pressable } from "@/components/ui/Pressable";

const MAX_NAME = 40;
const MAX_DESCRIPTION = 500;
const MIN_RADIUS_M = 20;
const MAX_RADIUS_M = 2000;
const RADIUS_STEP_M = 10;
const DEFAULT_RADIUS_M = 40;

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

export type OldCreateGroupScreenProps = {
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

export function OldCreateGroupScreen({
  candidates,
  location,
  locationError,
  onSubmit,
  onUploadIcon,
  onCancel,
}: OldCreateGroupScreenProps) {
  const router = useRouter();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<GroupVisibility>("open");
  const [persistent, setPersistent] = useState(false);
  const [radiusM, setRadiusM] = useState(DEFAULT_RADIUS_M);
  const [required, setRequired] = useState(1);
  const [inviteeIds, setInviteeIds] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [similar, setSimilar] = useState<SimilarGroup[]>([]);
  const [stale, setStale] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [iconState, setIconState] = useState<IconState>("idle");
  const iconInput = useRef<HTMLInputElement>(null);

  const quorumMax = Math.max(1, candidates.length);
  const quorum = Math.min(required, quorumMax);
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

  const changeIcon = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file || !onUploadIcon) {
      return;
    }
    setIconState("uploading");
    const mediaId = await onUploadIcon(file);
    setIconState(mediaId ? "ready" : "error");
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
          aria-label={
            iconState === "ready" ? "アイコンをアップロードしました" : "グループアイコンを選ぶ"
          }
          onClick={pickIcon}
        >
          <Icon name={iconState === "ready" ? "check" : "add_a_photo"} size={28} />
        </Pressable>
        <input
          ref={iconInput}
          type="file"
          accept="image/jpeg,image/png,image/webp"
          className={styles.fileInput}
          onChange={(event) => void changeIcon(event)}
        />
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
      </section>

      <div className={styles.band} />

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>エリアの広さ</h2>
        <p className={styles.sectionDesc}>
          今いる場所を中心にした円の半径です。この中にいる人が参加できます
        </p>
        <div className={styles.ratioRow}>
          <span className={styles.ratioValue}>{radiusM}m</span>
        </div>
        <Slider
          min={MIN_RADIUS_M}
          max={MAX_RADIUS_M}
          step={RADIUS_STEP_M}
          value={radiusM}
          tone="accent"
          label="エリアの半径"
          marks={["20m", "1km", "2km"]}
          onChange={setRadiusM}
        />
      </section>

      <div className={styles.band} />

      <section className={styles.section}>
        <div className={styles.toggleRow}>
          <div className={styles.toggleText}>
            <h2 className={styles.sectionTitle}>参加後もエリア外で使う</h2>
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
      </section>

      <div className={styles.band} />

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>公開範囲</h2>
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
            tone="accent"
            onSelect={() => setVisibility("closed")}
          />
        </div>
      </section>

      {visibility === "closed" && (
        <>
          <div className={styles.band} />
          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>承諾が必要な人数</h2>
            <p className={styles.sectionDesc}>
              招待した人のうち、この人数が承諾するとグループが作られます
            </p>
            <div className={styles.ratioRow}>
              <span className={styles.ratioValue}>{quorum}人</span>
              <span className={styles.ratioNote}>
                {candidates.length}人中 {quorum}人が承諾すれば作成
              </span>
            </div>
            <Slider
              min={1}
              max={quorumMax}
              step={1}
              value={quorum}
              tone="accent"
              label="承諾が必要な人数"
              marks={["1人", "全員"]}
              onChange={setRequired}
            />
          </section>
        </>
      )}

      <div className={styles.band} />

      <section className={styles.section}>
        <div className={styles.sectionHead}>
          <h2 className={styles.sectionTitle}>招待する人</h2>
          <span className={styles.count}>{inviteeIds.length}人</span>
        </div>
        <InviteePicker
          candidates={candidates}
          selectedIds={inviteeIds}
          onToggle={toggleInvitee}
        />
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
            アイコンをアップロードできませんでした。
          </Notice>
        </div>
      )}

      <div className={styles.notice}>
        {locationError ? (
          <Notice tone="warning" icon="location_off">
            位置情報を取得できませんでした（{locationError}）
          </Notice>
        ) : location ? (
          <Notice tone="info" icon="location_on">
            グループは今いる場所（半径{radiusM}m）に作られます
          </Notice>
        ) : (
          <Notice tone="info" icon="location_searching">
            現在地を取得しています…
          </Notice>
        )}
      </div>
    </div>
  );
}
