"use client";

import { useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { useAuth, WELCOME_PATH } from "@/auth";
import type { NotificationType, Settings, WarnDisplay } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { RevealDelaySetting } from "@/components/feature/settings";
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
   * unavailable (404・405・501) はインラインで出し分ける。
   */
  state: ResourceState<Settings>;
  /** 変更されたキーだけを通知する。永続化 (PATCH) は後続 todo。 */
  onChange?: (patch: Partial<Settings>) => void;
  /** error 状態の再取得。useResource の reload を渡す。 */
  onRetry?: () => void;
};

export function SettingsScreen({ state, onChange, onRetry }: SettingsScreenProps) {
  return (
    <AppShell>
      <ScreenHeader back title="設定" />

      <div className={styles.content}>
        {state.status === "ok" ? (
          <SettingsBody settings={state.data} onChange={onChange} />
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

        <AccountSection />
      </div>
    </AppShell>
  );
}

type SettingsBodyProps = {
  settings: Settings;
  onChange?: (patch: Partial<Settings>) => void;
};

function SettingsBody({ settings, onChange }: SettingsBodyProps) {
  const [current, setCurrent] = useState<Settings>(settings);

  const patch = (partial: Partial<Settings>) => {
    setCurrent((prev) => ({ ...prev, ...partial }));
    onChange?.(partial);
  };

  const notifyOn = (type: NotificationType) => current.notify[type] ?? true;

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
          encounterMode={current.encounterMode}
          encounterDelaySec={current.encounterDelaySec}
          onChange={(next) => patch(next)}
        />
      </section>

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>フレンドとパーティ</h2>
        <Card padding="md">
          <ToggleRow
            title="フレンドへのビーコン共有"
            note="近くにいるとき、フレンドに自分のビーコンを届けます"
            checked={current.friendBeaconShare}
            onChange={(friendBeaconShare) => patch({ friendBeaconShare })}
          />
          <div className={styles.divider} />
          <ToggleRow
            title="パーティに自動参加"
            note="近くのフレンドとすれ違ったとき、自動でパーティに入ります"
            checked={current.partyAuto}
            onChange={(partyAuto) => patch({ partyAuto })}
          />
          <div className={styles.divider} />
          <ToggleRow
            title="位置共有の一時停止"
            note="オンにすると、位置の共有とすれ違いの記録を止めます"
            checked={current.locationPaused}
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
            value={current.warnDisplay}
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

function AccountSection() {
  const [loggingOut, setLoggingOut] = useState(false);
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
