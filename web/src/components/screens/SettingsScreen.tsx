"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useAuth, WELCOME_PATH } from "@/auth";
import type { LocationVisibility, Settings } from "@/domain/types";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { RevealDelaySetting } from "@/components/feature/settings";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Chip } from "@/components/ui/Chip";
import { Notice } from "@/components/ui/Notice";
import { Toggle } from "@/components/ui/Toggle";
import styles from "./SettingsScreen.module.css";

const VISIBILITY_OPTIONS: { value: LocationVisibility; label: string }[] = [
  { value: "area", label: "エリア名" },
  { value: "direction", label: "方角だけ" },
  { value: "hidden", label: "見せない" },
];

export type SettingsScreenProps = {
  settings: Settings;
  onChange?: (settings: Settings) => void;
};

export function SettingsScreen({ settings, onChange }: SettingsScreenProps) {
  const [current, setCurrent] = useState<Settings>(settings);
  const [loggingOut, setLoggingOut] = useState(false);
  const router = useRouter();
  const { logout } = useAuth();

  // TODO(todo 9): 位置の見せ方カードの書き直しまでは旧 mock フィールドを安全に読むだけ。
  const legacySharedAreas = (
    current as unknown as { sharedAreas?: string[] }
  ).sharedAreas;

  const patch = (partial: Partial<Settings>) => {
    const next = { ...current, ...partial };
    setCurrent(next);
    onChange?.(next);
  };

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
    <AppShell>
      <ScreenHeader back title="設定" />

      <div className={styles.content}>
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
          <h2 className={styles.sectionTitle}>友達申請</h2>
          <Card padding="md">
            <div className={styles.row}>
              <div className={styles.rowText}>
                <span className={styles.rowTitle}>友達申請を許可</span>
                <span className={styles.rowNote}>
                  オフにすると、新しい申請は届きません
                </span>
              </div>
              <Toggle
                checked={current.allowFriendRequests}
                onChange={(allowFriendRequests) => patch({ allowFriendRequests })}
                label="友達申請を許可"
              />
            </div>
          </Card>
        </section>

        <section className={styles.section}>
          <h2 className={styles.sectionTitle}>友達への位置の見せ方</h2>
          <Card padding="md">
            <div className={styles.chipRow}>
              {VISIBILITY_OPTIONS.map((option) => (
                <Chip
                  key={option.value}
                  label={option.label}
                  surface="surface"
                  selected={current.locationVisibility === option.value}
                  onClick={() => patch({ locationVisibility: option.value })}
                  className={styles.chip}
                />
              ))}
            </div>
            <div className={styles.divider} />
            <div className={styles.row}>
              <div className={styles.rowText}>
                <span className={styles.rowTitle}>決めたエリアだけで共有</span>
                <span className={styles.rowNote}>
                  {legacySharedAreas
                    ? `${legacySharedAreas.join("・")}の${legacySharedAreas.length}か所`
                    : ""}
                </span>
              </div>
              <Toggle
                checked={current.restrictToAreas}
                onChange={(restrictToAreas) => patch({ restrictToAreas })}
                label="決めたエリアだけで共有"
              />
            </div>
          </Card>
        </section>

        <div className={styles.footer}>
          <Notice tone="info" icon="lock">
            DMはありません。会話はパーティとグループの中だけ
          </Notice>
        </div>

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
      </div>
    </AppShell>
  );
}
