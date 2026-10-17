"use client";

import type { Pin, Post } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import styles from "./PinList.module.css";

export type PinListProps = {
  /** GET /instances/{spaceId}/pins の取得状態 (10 §6)。 */
  state: ResourceState<Pin[]>;
  /** 取得済みの Pin (新しい順)。 */
  pins: Pin[];
  /** タイムラインの投稿。手動 Pin の候補と本文の解決に使う。 */
  posts: Post[];
  /** 投稿を手動 Pin する。 */
  onPin: (post: Post) => void;
  /** Pin を外す。 */
  onUnpin: (pin: Pin) => void;
  /** error 状態の再取得。 */
  onRetry?: () => void;
  /** Pin / 解除の送信中。ボタンを二重押下させない。 */
  busy?: boolean;
};

const REASON_LABEL: Record<Pin["reason"], string> = {
  auto: "自動ピン",
  manual: "手動ピン",
};

function excerpt(body: string): string {
  return body.length > 60 ? `${body.slice(0, 60)}…` : body;
}

export function PinList({
  state,
  pins,
  posts,
  onPin,
  onUnpin,
  onRetry,
  busy = false,
}: PinListProps) {
  if (state.status === "loading") {
    return (
      <Card padding="md" className={styles.empty}>
        読み込み中…
      </Card>
    );
  }

  if (state.status === "error") {
    return (
      <Card padding="md" className={styles.stack}>
        <p className={styles.empty}>ピンを読み込めませんでした</p>
        {onRetry && (
          <Button variant="secondary" icon="refresh" onClick={onRetry}>
            再読み込み
          </Button>
        )}
      </Card>
    );
  }

  // unavailable (404・405・501) は「まだ無い / 未対応」。偽の Pin は出さない。
  if (state.status === "unavailable") {
    return (
      <Card padding="md" className={styles.empty}>
        ピンはまだ利用できません
      </Card>
    );
  }

  const pinnedSubjects = new Set(pins.map((pin) => pin.subject));
  const candidates = posts.filter((post) => !pinnedSubjects.has(post.id));
  const bodyFor = (subject: string) =>
    posts.find((post) => post.id === subject)?.body ?? "";

  return (
    <>
      {pins.length === 0 ? (
        <Card padding="md" className={styles.empty}>
          まだピンされた投稿はありません
        </Card>
      ) : (
        pins.map((pin) => {
          const body = bodyFor(pin.subject);
          return (
            <Card key={pin.uri} padding="md" className={styles.item}>
              <div className={styles.head}>
                <Icon name="push_pin" size={16} />
                <span className={styles.reason}>{REASON_LABEL[pin.reason]}</span>
              </div>
              {body ? (
                <p className={styles.body}>{excerpt(body)}</p>
              ) : (
                <p className={styles.subject}>{pin.subject}</p>
              )}
              <div className={styles.actions}>
                <Button
                  variant="ghost"
                  size="sm"
                  icon="delete"
                  onClick={() => onUnpin(pin)}
                  disabled={busy}
                >
                  ピンを外す
                </Button>
              </div>
            </Card>
          );
        })
      )}

      <p className={styles.sectionTitle}>ピンする投稿を選ぶ</p>
      {candidates.length === 0 ? (
        <Card padding="md" className={styles.empty}>
          ピンできる投稿がありません
        </Card>
      ) : (
        candidates.map((post) => (
          <Card key={post.id} padding="md" className={styles.item}>
            <p className={styles.body}>{excerpt(post.body)}</p>
            <div className={styles.actions}>
              <Button
                variant="secondary"
                size="sm"
                icon="push_pin"
                onClick={() => onPin(post)}
                disabled={busy}
              >
                ピンする
              </Button>
            </div>
          </Card>
        ))
      )}
    </>
  );
}
