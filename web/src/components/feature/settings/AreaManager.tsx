"use client";

import { useState, type FormEvent } from "react";
import type { Area } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { IconButton } from "@/components/ui/IconButton";
import { Notice } from "@/components/ui/Notice";
import { Slider } from "@/components/ui/Slider";
import styles from "./AreaManager.module.css";

/** backend の上限 (07 §4.3): エリアは 1 人 10 個、半径 50〜5000 m、label は 20 文字。 */
export const MAX_AREAS = 10;
export const MIN_AREA_RADIUS_M = 50;
export const MAX_AREA_RADIUS_M = 5000;
export const AREA_RADIUS_STEP_M = 50;
export const MAX_AREA_LABEL_LEN = 20;

const DEFAULT_RADIUS_M = 500;
const RADIUS_MARKS = ["50m", "2.5km", "5km"];

/** POST /me/areas に送る画面の入力。中心の現在地はページ側が足す。 */
export type AreaDraft = { label: string; radiusM: number };

/** エリアの作成・削除の結果。画面は ok 以外を文言で出し分ける。 */
export type AreaMutationResult =
  | { status: "ok" }
  | { status: "unavailable" }
  | { status: "error"; message: string };

export type AreaManagerProps = {
  /** GET /me/areas の取得状態 (loading / ok / empty / error / unavailable)。 */
  state: ResourceState<Area[]>;
  /** 取得済みのエリア (ページを跨いで蓄積)。 */
  items: Area[];
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
  /** 現在地。null の間はエリアを作れない。 */
  location: { lat: number; lon: number } | null;
  /** 現在地を取れない理由。 */
  locationError?: string | null;
  /** error 状態の再取得。 */
  onRetry?: () => void;
  /** 現在地を中心にエリアを作る。 */
  onCreate?: (draft: AreaDraft) => Promise<AreaMutationResult>;
  /** エリアを消す。 */
  onDelete?: (id: string) => Promise<AreaMutationResult>;
};

export function AreaManager({
  state,
  items,
  hasMore,
  loadingMore,
  onLoadMore,
  location,
  locationError,
  onRetry,
  onCreate,
  onDelete,
}: AreaManagerProps) {
  const [label, setLabel] = useState("");
  const [radiusM, setRadiusM] = useState(DEFAULT_RADIUS_M);
  const [submitting, setSubmitting] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const atLimit = items.length >= MAX_AREAS;
  const canCreate =
    onCreate !== undefined &&
    location !== null &&
    !atLimit &&
    label.trim().length > 0 &&
    !submitting;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!canCreate || !onCreate) {
      return;
    }
    setSubmitting(true);
    setError(null);
    const result = await onCreate({ label: label.trim(), radiusM });
    setSubmitting(false);
    if (result.status === "ok") {
      setLabel("");
      setRadiusM(DEFAULT_RADIUS_M);
      return;
    }
    setError(
      result.status === "unavailable" ? "エリアはまだ利用できません。" : result.message,
    );
  };

  const remove = async (area: Area) => {
    if (!onDelete || deletingId !== null) {
      return;
    }
    setDeletingId(area.id);
    setError(null);
    const result = await onDelete(area.id);
    setDeletingId(null);
    if (result.status === "unavailable") {
      setError("エリアはまだ利用できません。");
      return;
    }
    if (result.status === "error") {
      setError(result.message);
    }
  };

  return (
    <section className={styles.section}>
      <div className={styles.sectionHead}>
        <h2 className={styles.sectionTitle}>エリア</h2>
        <p className={styles.sectionDesc}>
          よくいる場所を登録し、エリア精度で位置を共有している相手に名前を見せます
        </p>
      </div>

      <Card padding="md">
        <AreaList
          state={state}
          items={items}
          onRetry={onRetry}
          onDelete={onDelete ? remove : undefined}
          deletingId={deletingId}
        />
        {hasMore && onLoadMore && (
          <div className={styles.more}>
            <Button variant="ghost" size="sm" onClick={onLoadMore} disabled={loadingMore}>
              {loadingMore ? "読み込み中…" : "もっと見る"}
            </Button>
          </div>
        )}
      </Card>

      <Card padding="md">
        <form className={styles.form} onSubmit={(event) => void submit(event)}>
          <div className={styles.field}>
            <label className={styles.fieldLabel} htmlFor="area-label">
              エリア名
            </label>
            <input
              id="area-label"
              className={styles.input}
              value={label}
              maxLength={MAX_AREA_LABEL_LEN}
              placeholder="例: 図書館"
              disabled={atLimit}
              onChange={(event) => setLabel(event.target.value)}
            />
            <span className={styles.counter}>
              {label.length}/{MAX_AREA_LABEL_LEN}
            </span>
          </div>

          <div className={styles.field}>
            <span className={styles.fieldLabel}>半径</span>
            <div className={styles.radiusRow}>
              <span className={styles.radiusValue}>{radiusM}m</span>
            </div>
            <Slider
              min={MIN_AREA_RADIUS_M}
              max={MAX_AREA_RADIUS_M}
              step={AREA_RADIUS_STEP_M}
              value={radiusM}
              label="エリアの半径"
              marks={RADIUS_MARKS}
              onChange={setRadiusM}
            />
          </div>

          <div className={styles.actions}>
            <Button variant="secondary" size="sm" icon="add" type="submit" disabled={!canCreate}>
              {submitting ? "追加しています…" : "追加"}
            </Button>
          </div>

          {atLimit && (
            <p className={styles.limit} role="status">
              エリアは{MAX_AREAS}個までです
            </p>
          )}
          {!atLimit && !locationError && location === null && (
            <p className={styles.hint} role="status">
              現在地を取得しています…
            </p>
          )}
          {locationError && (
            <Notice tone="warning" icon="location_off">
              現在地を取得できませんでした（{locationError}）
            </Notice>
          )}
          {error && (
            <div role="alert">
              <Notice tone="warning" icon="error">
                {error}
              </Notice>
            </div>
          )}
        </form>
      </Card>

      <Notice tone="info" icon="info">
        エリア名は、エリア精度で位置を共有している相手にだけ見えます
      </Notice>
    </section>
  );
}

type AreaListProps = {
  state: ResourceState<Area[]>;
  items: Area[];
  onRetry?: () => void;
  onDelete?: (area: Area) => void;
  deletingId: string | null;
};

function AreaList({ state, items, onRetry, onDelete, deletingId }: AreaListProps) {
  if (state.status === "loading") {
    return (
      <p className={styles.listState} role="status">
        読み込んでいます…
      </p>
    );
  }
  if (state.status === "error") {
    return (
      <div className={styles.listError} role="alert">
        <Notice tone="warning" icon="cloud_off">
          エリアを読み込めませんでした
        </Notice>
        {onRetry && (
          <Button variant="secondary" size="sm" icon="refresh" onClick={onRetry}>
            再読み込み
          </Button>
        )}
      </div>
    );
  }
  if (state.status === "unavailable") {
    return (
      <p className={styles.listState} role="status">
        エリアは準備中です
      </p>
    );
  }
  if (items.length === 0) {
    return (
      <p className={styles.listState} role="status">
        まだエリアはありません
      </p>
    );
  }
  return (
    <ul className={styles.list}>
      {items.map((area) => (
        <li key={area.id} className={styles.row}>
          <span className={styles.rowIcon} aria-hidden>
            <Icon name="place" size={20} />
          </span>
          <span className={styles.rowMain}>
            <span className={styles.rowLabel}>{area.label}</span>
            <span className={styles.rowMeta}>半径 {area.radiusM}m</span>
          </span>
          {onDelete && (
            <IconButton
              icon="delete"
              label={`エリア「${area.label}」を削除`}
              disabled={deletingId === area.id}
              onClick={() => onDelete(area)}
            />
          )}
        </li>
      ))}
    </ul>
  );
}
