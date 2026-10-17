"use client";

import { useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { useAuth, WELCOME_PATH } from "@/auth";
import type { ApiError } from "@/auth/errors";
import type { NotificationType, Settings, WarnDisplay } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import {
  AreaManager,
  RevealDelaySetting,
  type AreaManagerProps,
} from "@/components/feature/settings";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { Notice } from "@/components/ui/Notice";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Toggle } from "@/components/ui/Toggle";
import styles from "./SettingsScreen.module.css";

/** moderation 警告の表示方法 (11 §1)。既定は "blur"。 */
const WARN_OPTIONS: { value: WarnDisplay; label: string }[] = [
  { value: "show", label: "そのまま表示" },
  { value: "blur", label: "ぼかす" },
  { value: "hide", label: "隠す" },
];

/**
 * 通知の種類 (12 §3)。既定はすべて on で、user_settings には既定から変えた
 * 種類だけが入る (notify[type] が無い = on)。
 */
const NOTIFY_ITEMS: { type: NotificationType; label: string }[] = [
  { type: "friend.request", label: "フレンド申請" },
  { type: "friend.accepted", label: "フレンドの承認" },
  { type: "party.confirmed", label: "パーティの確定" },
  { type: "group.nearby", label: "近くのグループ" },
  { type: "group.invited", label: "グループへの招待" },
  { type: "group.suggested", label: "グループの提案" },
  { type: "encounter.daily", label: "すれ違い（毎日）" },
  { type: "encounter.weekly", label: "すれ違い（毎週）" },
  { type: "encounter.monthly", label: "すれ違い（毎月）" },
  { type: "moderation.hidden", label: "投稿の非表示" },
];

/** backend が false を弾く種類 (12 §3)。行はオン固定で切替不可にする。 */
const NOTIFY_LOCKED: NotificationType = "moderation.hidden";

export type SettingsScreenProps = {
  /**
   * GET /me の設定の取得状態。ok のときだけ本体を描画し、loading / error /
   * unavailable (404・405・501) はインラインで出し分ける。ok の data は
   * 楽観更新が反映された現在値 (useSettingsPersistence の戻り)。
   */
  state: ResourceState<Settings>;
  /** 変更されたキーだけを通知する。永続化は useSettingsPersistence。 */
  onChange?: (patch: Partial<Settings>) => void;
  /** 保存中の小さな進捗表示。 */
  saving?: boolean;
  /** 直近の保存失敗。あれば警告と再試行を出す。 */
  saveError?: ApiError | null;
  /** 失敗した保存の再送。 */
  onRetrySave?: () => void;
  /** error 状態の再取得。useResource の reload を渡す。 */
  onRetry?: () => void;
  /** エリア管理 (07 §4.3)。未指定なら節を出さない。 */
  areas?: AreaManagerProps;
  /** アカウント削除 (13 §5)。未指定なら節を出さない。 */
  onDeleteAccount?: (opts: { deletePdsRecords: boolean }) => Promise<AccountDeleteResult>;
};

/** アカウント削除 (DELETE /me, 13 §5) の結果。 */
export type AccountDeleteResult =
  | { status: "ok" }
  | { status: "unavailable" }
  | { status: "error"; message: string };

/** 破壊的操作の確認語。これと一致しないかぎり削除できない。 */
const DELETE_CONFIRM_WORD = "削除";

export function SettingsScreen({
  state,
  onChange,
  saving,
  saveError,
  onRetrySave,
  onRetry,
  areas,
  onDeleteAccount,
}: SettingsScreenProps) {
  return (
    <AppShell>
      <ScreenHeader back title="設定" />

      <div className={styles.content}>
        {state.status === "ok" ? (
          <>
            <SaveStatus saving={saving} error={saveError} onRetry={onRetrySave} />
            <SettingsBody settings={state.data} onChange={onChange} />
          </>
        ) : state.status === "loading" ? (
          <SettingsSkeleton />
        ) : state.status === "error" ? (
          <SettingsState
            icon="cloud_off"
            title="設定を読み込めませんでした"
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
          // unavailable (404・405・501) は「まだ無い / 未対応」。
          <SettingsState
            icon="construction"
            title="設定は準備中です"
            description="この機能はまだ提供されていません。"
          />
        )}

        {areas && <AreaManager {...areas} />}

        <AccountSection onDeleteAccount={onDeleteAccount} />
      </div>
    </AppShell>
  );
}

type SettingsBodyProps = {
  settings: Settings;
  onChange?: (patch: Partial<Settings>) => void;
};

type SaveStatusProps = {
  saving?: boolean;
  error?: ApiError | null;
  onRetry?: () => void;
};

function SaveStatus({ saving, error, onRetry }: SaveStatusProps) {
  if (error) {
    return (
      <div className={styles.saveError} role="alert">
        <Notice tone="warning" icon="cloud_off">
          設定を保存できませんでした
        </Notice>
        {onRetry && (
          <Button variant="secondary" size="sm" icon="refresh" onClick={onRetry}>
            再試行
          </Button>
        )}
      </div>
    );
  }
  if (saving) {
    return (
      <p className={styles.saveNote} role="status">
        保存しています…
      </p>
    );
  }
  return null;
}

function SettingsBody({ settings, onChange }: SettingsBodyProps) {
  const patch = (partial: Partial<Settings>) => {
    onChange?.(partial);
  };

  const notifyOn = (type: NotificationType) => settings.notify[type] ?? true;

  const setNotify = (type: NotificationType, on: boolean) => {
    // backend は true を削除して「既定に戻す」ため、変えた 1 種類だけを送る (12 §3)。
    patch({ notify: { [type]: on } });
  };

  return (
    <>
      <section className={styles.section}>
        <div className={styles.sectionHead}>
          <h2 className={styles.sectionTitle}>すれ違いの公開</h2>
          <p className={styles.sectionDesc}>
            すれ違った相手が、あなたを見られるタイミング
          </p>
        </div>
        <RevealDelaySetting
          encounterMode={settings.encounterMode}
          encounterDelaySec={settings.encounterDelaySec}
          onChange={(next) => patch(next)}
        />
      </section>

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>フレンドとパーティ</h2>
        <Card padding="md">
          <ToggleRow
            title="フレンドへのビーコン共有"
            note="近くにいるとき、フレンドに自分のビーコンを届けます"
            checked={settings.friendBeaconShare}
            onChange={(friendBeaconShare) => patch({ friendBeaconShare })}
          />
          <div className={styles.divider} />
          <ToggleRow
            title="パーティに自動参加"
            note="近くのフレンドとすれ違ったとき、自動でパーティに入ります"
            checked={settings.partyAuto}
            onChange={(partyAuto) => patch({ partyAuto })}
          />
          <div className={styles.divider} />
          <ToggleRow
            title="位置共有の一時停止"
            note="オンにすると、位置の共有とすれ違いの記録を止めます"
            checked={settings.locationPaused}
            onChange={(locationPaused) => patch({ locationPaused })}
          />
        </Card>
      </section>

      <section className={styles.section}>
        <div className={styles.sectionHead}>
          <h2 className={styles.sectionTitle}>警告の表示方法</h2>
          <p className={styles.sectionDesc}>
            モデレーションで警告が付いた投稿の見え方
          </p>
        </div>
        <Card padding="md">
          <SegmentedControl
            options={WARN_OPTIONS}
            value={settings.warnDisplay}
            onChange={(warnDisplay) => patch({ warnDisplay })}
            track="bg"
          />
        </Card>
      </section>

      <section className={styles.section}>
        <div className={styles.sectionHead}>
          <h2 className={styles.sectionTitle}>通知の種類</h2>
          <p className={styles.sectionDesc}>受け取りたい通知を選べます</p>
        </div>
        <Card padding="md">
          {NOTIFY_ITEMS.map((item, index) => {
            const locked = item.type === NOTIFY_LOCKED;
            return (
              <div key={item.type}>
                {index > 0 && <div className={styles.divider} />}
                <ToggleRow
                  title={item.label}
                  note={locked ? "この通知はオフにできません" : undefined}
                  checked={locked ? true : notifyOn(item.type)}
                  disabled={locked}
                  onChange={(on) => setNotify(item.type, on)}
                />
              </div>
            );
          })}
        </Card>
      </section>

      <div className={styles.footer}>
        <Notice tone="info" icon="lock">
          DMはありません。会話はパーティとグループの中だけ
        </Notice>
      </div>
    </>
  );
}

type ToggleRowProps = {
  title: string;
  note?: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
};

function ToggleRow({
  title,
  note,
  checked,
  disabled,
  onChange,
}: ToggleRowProps) {
  return (
    <div className={styles.row}>
      <div className={styles.rowText}>
        <span className={styles.rowTitle}>{title}</span>
        {note && <span className={styles.rowNote}>{note}</span>}
      </div>
      <Toggle
        checked={checked}
        onChange={onChange}
        label={title}
        disabled={disabled}
      />
    </div>
  );
}

type AccountSectionProps = {
  onDeleteAccount?: (opts: { deletePdsRecords: boolean }) => Promise<AccountDeleteResult>;
};

function AccountSection({ onDeleteAccount }: AccountSectionProps) {
  const [loggingOut, setLoggingOut] = useState(false);
  const [deletePdsRecords, setDeletePdsRecords] = useState(false);
  const [confirm, setConfirm] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const router = useRouter();
  const { logout } = useAuth();

  const handleLogout = async () => {
    if (loggingOut) {
      return;
    }
    setLoggingOut(true);
    try {
      await logout();
    } finally {
      router.replace(WELCOME_PATH);
    }
  };

  const canDelete =
    onDeleteAccount !== undefined && confirm === DELETE_CONFIRM_WORD && !deleting;

  const handleDelete = async () => {
    if (!onDeleteAccount || !canDelete) {
      return;
    }
    setDeleting(true);
    setDeleteError(null);
    const result = await onDeleteAccount({ deletePdsRecords });
    if (result.status === "ok") {
      // backend 側でセッションは失効済み。手元を消して welcome へ戻す。
      await logout();
      router.replace(WELCOME_PATH);
      return;
    }
    setDeleting(false);
    setDeleteError(
      result.status === "unavailable"
        ? "アカウント削除はまだ利用できません。"
        : result.message,
    );
  };

  return (
    <section className={styles.section}>
      <h2 className={styles.sectionTitle}>アカウント</h2>
      <Card padding="md">
        <div className={styles.row}>
          <div className={styles.rowText}>
            <span className={styles.rowTitle}>ログアウト</span>
            <span className={styles.rowNote}>この端末からログアウトします</span>
          </div>
          <Button
            variant="secondary"
            size="sm"
            icon="logout"
            onClick={handleLogout}
            disabled={loggingOut}
          >
            {loggingOut ? "ログアウトしています…" : "ログアウト"}
          </Button>
        </div>

        {onDeleteAccount && (
          <>
            <div className={styles.divider} />
            <div className={styles.deleteBlock}>
              <div className={styles.rowText}>
                <span className={styles.rowTitle}>アカウントを削除</span>
                <span className={styles.rowNote}>
                  プロフィール・すれ違い・参加中のグループなどが消えます。取り消せません
                </span>
              </div>

              <ToggleRow
                title="Bluesky 上の投稿も削除"
                note="あなたの PDS にある CHAIN のレコードを削除します"
                checked={deletePdsRecords}
                onChange={setDeletePdsRecords}
                disabled={deleting}
              />

              <div className={styles.field}>
                <label className={styles.fieldLabel} htmlFor="delete-confirm">
                  確認のため「{DELETE_CONFIRM_WORD}」と入力してください
                </label>
                <input
                  id="delete-confirm"
                  className={styles.input}
                  value={confirm}
                  disabled={deleting}
                  onChange={(event) => setConfirm(event.target.value)}
                />
              </div>

              <div className={styles.actions}>
                <Button
                  variant="secondary"
                  size="sm"
                  icon="delete_forever"
                  onClick={() => void handleDelete()}
                  disabled={!canDelete}
                >
                  {deleting ? "削除しています…" : "アカウントを削除"}
                </Button>
              </div>

              {deleteError && (
                <div role="alert">
                  <Notice tone="warning" icon="error">
                    {deleteError}
                  </Notice>
                </div>
              )}
            </div>
          </>
        )}
      </Card>
    </section>
  );
}

type SettingsStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function SettingsState({ icon, title, description, action }: SettingsStateProps) {
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

function SettingsSkeleton() {
  return (
    <div className={styles.skeleton} aria-hidden>
      <div className={styles.skeletonBlock} />
      <div className={styles.skeletonBlock} />
    </div>
  );
}
