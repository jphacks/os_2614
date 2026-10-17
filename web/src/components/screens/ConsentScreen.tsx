"use client";

import type { ReactNode } from "react";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { Notice } from "@/components/ui/Notice";
import styles from "./ConsentScreen.module.css";

/** GET /me の settings.contentScanConsentVersion と PUT /me/consents の consentedAt。 */
export type ConsentState = {
  version?: number;
  consentedAt?: string | null;
};

export type ConsentScreenProps = {
  /** GET /me から読んだ同意の現在値。 */
  state: ResourceState<ConsentState>;
  saving?: boolean;
  error?: string | null;
  /** PUT /me/consents {contentScan:1} (02 §8)。 */
  onAgree: () => void;
  /** PUT /me/consents {contentScan:0} (撤回)。 */
  onWithdraw: () => void;
  onRetry?: () => void;
};

export function ConsentScreen({
  state,
  saving = false,
  error = null,
  onAgree,
  onWithdraw,
  onRetry,
}: ConsentScreenProps) {
  const consented = state.status === "ok" && state.data.version === 1;

  return (
    <AppShell>
      <ScreenHeader back title="内容チェックへの同意" />

      <div className={styles.content}>
        {state.status === "ok" ? (
          <>
            <Card padding="md">
              <p className={styles.lead}>
                投稿する前に、誹謗中傷や危険な内容が含まれていないかを AI が自動で確認します。
                判定の結果によって、投稿が非表示になったり、注意書きが付いたりします。
              </p>
              {consented ? (
                <p className={styles.status} data-state="agreed">
                  <Icon name="verified_user" size={18} />
                  同意済み
                  {state.data.consentedAt
                    ? `（${new Date(state.data.consentedAt).toLocaleDateString("ja-JP")}）`
                    : ""}
                </p>
              ) : (
                <p className={styles.status} data-state="pending">
                  <Icon name="error" size={18} />
                  まだ同意していません
                </p>
              )}
              <div className={styles.actions}>
                {consented ? (
                  <Button
                    variant="secondary"
                    onClick={onWithdraw}
                    disabled={saving}
                  >
                    {saving ? "更新中…" : "同意を撤回する"}
                  </Button>
                ) : (
                  <Button variant="primary" onClick={onAgree} disabled={saving}>
                    {saving ? "更新中…" : "同意する"}
                  </Button>
                )}
              </div>
            </Card>

            <Notice tone="info" icon="info">
              撤回しても、これまでの投稿の閲覧と公開フィードへの投稿はできます。グループや
              パーティ（Space）への新しい投稿だけができなくなります。
            </Notice>

            {error && (
              <p className={styles.error} role="alert">
                {error}
              </p>
            )}
          </>
        ) : state.status === "loading" ? (
          <ConsentState icon="hourglass" title="読み込み中…" description="" />
        ) : state.status === "error" ? (
          <ConsentState
            icon="cloud_off"
            title="同意の状態を読み込めませんでした"
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
          <ConsentState
            icon="construction"
            title="同意は準備中です"
            description="この機能はまだ提供されていません。"
          />
        )}
      </div>
    </AppShell>
  );
}

type ConsentStateViewProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function ConsentState({ icon, title, description, action }: ConsentStateViewProps) {
  return (
    <div className={styles.state}>
      <span className={styles.stateIcon} aria-hidden>
        <Icon name={icon} size={32} />
      </span>
      <p className={styles.stateTitle}>{title}</p>
      {description && <p className={styles.stateDescription}>{description}</p>}
      {action}
    </div>
  );
}
