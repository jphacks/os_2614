"use client";

import { useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { useAuth, WELCOME_PATH } from "@/auth";
import type { DataMap } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { Notice } from "@/components/ui/Notice";
import styles from "./DataMapScreen.module.css";

/** PDS 側で CHAIN の認可を取り消す場所 (Bluesky のアプリの設定)。 */
const BLUESKY_SETTINGS_URL = "https://bsky.app/settings";

const STORAGE_LABELS: Record<DataMap["rows"][number]["storage"], string> = {
  pds: "あなたの PDS",
  chain: "CHAIN",
  external: "外部",
};

export type DataMapScreenProps = {
  /** GET /me/data-map (O4、19 §7.1) の取得状態。 */
  state: ResourceState<DataMap>;
  onRetry?: () => void;
};

export function DataMapScreen({ state, onRetry }: DataMapScreenProps) {
  return (
    <AppShell>
      <ScreenHeader back="/settings/" title="データの置き場所" />
      {state.status === "ok" ? (
        <DataMapBody map={state.data} />
      ) : state.status === "loading" ? (
        <div className={styles.skeleton} aria-hidden>
          {[0, 1, 2].map((row) => (
            <div key={row} className={styles.skeletonRow} />
          ))}
        </div>
      ) : state.status === "error" ? (
        <DataMapState
          icon="cloud_off"
          title="データの置き場所を読み込めませんでした"
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
        <DataMapState
          icon="construction"
          title="データの置き場所は準備中です"
          description="この機能はまだ提供されていません。"
        />
      )}
    </AppShell>
  );
}

function DataMapBody({ map }: { map: DataMap }) {
  return (
    <>
      <div className={styles.intro}>
        <Notice tone="info" icon="database">
          {map.pdsHost
            ? `あなたのデータは ${map.pdsHost} にあります。種類ごとに、どこにあり、誰が見られるかを示します。`
            : "種類ごとに、どこにあり、誰が見られるかを示します。"}
        </Notice>
      </div>

      <section className={styles.section} aria-label="データの一覧">
        <ul className={styles.rows}>
          {map.rows.map((row) => (
            <li key={row.id} data-testid={`data-row-${row.id}`}>
              <Card padding="md">
                <div className={styles.rowHead}>
                  <h2 className={styles.rowTitle}>{row.data}</h2>
                  <span className={`${styles.badge} ${styles[row.storage]}`}>
                    {STORAGE_LABELS[row.storage]}
                  </span>
                </div>
                <dl className={styles.facts}>
                  <dt>置き場所</dt>
                  <dd>{row.place}</dd>
                  <dt>見られる人</dt>
                  <dd>{row.visibleTo}</dd>
                  <dt>保存期間</dt>
                  <dd>{row.retention}</dd>
                  <dt>持ち出し</dt>
                  <dd>{row.export}</dd>
                  {row.counts.length > 0 && (
                    <>
                      <dt>件数</dt>
                      <dd>
                        {row.counts
                          .map((count) => `${count.label} ${count.value}件`)
                          .join("・")}
                      </dd>
                    </>
                  )}
                </dl>
              </Card>
            </li>
          ))}
        </ul>
      </section>

      <ScopeSection scopes={map.scopes} />
      <RevokeSection />
    </>
  );
}

function ScopeSection({ scopes }: { scopes: string[] | null }) {
  return (
    <section className={styles.section}>
      <h2 className={styles.sectionTitle}>CHAIN に許可している権限</h2>
      <Card padding="md">
        {scopes === null ? (
          <p className={styles.note} data-testid="scopes-unknown">
            ログインの有効期限が切れているため、許可している権限を確認できません。もう一度ログインしてください。
          </p>
        ) : scopes.length === 0 ? (
          <p className={styles.note}>許可している権限はありません。</p>
        ) : (
          <ul className={styles.scopes} data-testid="scopes">
            {scopes.map((scope) => (
              <li key={scope}>
                <code>{scope}</code>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </section>
  );
}

function RevokeSection() {
  const router = useRouter();
  const { logout } = useAuth();
  const [loggingOut, setLoggingOut] = useState(false);

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
      <h2 className={styles.sectionTitle}>CHAIN へのアクセスを取り消す</h2>
      <Card padding="md">
        <p className={styles.note}>
          CHAIN に与えた権限は、Bluesky のアプリの設定からいつでも取り消せます。取り消すと、CHAIN
          はあなたの PDS に書き込めなくなります。この端末のログインだけをやめる場合は、ログアウトしてください。
        </p>
        <div className={styles.actions}>
          <a
            className={styles.externalLink}
            href={BLUESKY_SETTINGS_URL}
            target="_blank"
            rel="noopener noreferrer"
          >
            Bluesky の設定を開く
            <Icon name="open_in_new" size={16} />
          </a>
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

type DataMapStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function DataMapState({ icon, title, description, action }: DataMapStateProps) {
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
