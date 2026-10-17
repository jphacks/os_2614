"use client";

import { useCallback, useState, type ReactNode } from "react";
import type { Block } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { List, ListItem } from "@/components/ui/List";
import { Notice } from "@/components/ui/Notice";
import styles from "./BlocksScreen.module.css";

/** POST /blocks (13 §4) の結果。409 はすでにブロック済み。 */
export type BlockAddResult =
  | { status: "ok" }
  | { status: "conflict" }
  | { status: "unavailable" }
  | { status: "error"; message: string };

export type BlocksScreenProps = {
  /** GET /blocks (13 §4) の取得状態。 */
  state: ResourceState<Block[]>;
  /** ブロックした相手の表示補助 (未登録は DID のまま)。 */
  onAdd: (did: string) => Promise<BlockAddResult>;
  /** DELETE /blocks/{did} (13 §4)。成功しなければ reject する。 */
  onRemove: (did: string) => Promise<void>;
  onRetry?: () => void;
};

export function BlocksScreen({ state, onAdd, onRemove, onRetry }: BlocksScreenProps) {
  const [draft, setDraft] = useState("");
  const [adding, setAdding] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [removing, setRemoving] = useState<ReadonlySet<string>>(new Set());

  const submit = useCallback(async () => {
    const did = draft.trim();
    if (!did || adding) {
      return;
    }
    setMessage(null);
    setAdding(true);
    const result = await onAdd(did);
    setAdding(false);
    if (result.status === "ok") {
      setDraft("");
      return;
    }
    if (result.status === "conflict") {
      setMessage("この相手はすでにブロックしています。");
      return;
    }
    if (result.status === "unavailable") {
      setMessage("ブロックはまだ利用できません。");
      return;
    }
    setMessage(result.message);
  }, [adding, draft, onAdd]);

  const remove = useCallback(
    async (block: Block) => {
      setMessage(null);
      setRemoving((prev) => new Set(prev).add(block.did));
      try {
        await onRemove(block.did);
      } catch {
        setRemoving((prev) => {
          const next = new Set(prev);
          next.delete(block.did);
          return next;
        });
        setMessage(`${block.name}さんを解除できませんでした。`);
      }
    },
    [onRemove],
  );

  const blocks =
    state.status === "ok" ? state.data.filter((block) => !removing.has(block.did)) : [];

  return (
    <AppShell>
      <ScreenHeader
        back="/settings/"
        title="ブロック"
        count={state.status === "ok" ? `${blocks.length}人` : undefined}
      />

      <div className={styles.intro}>
        <Notice tone="info" icon="block">
          ブロックした相手には通知されません。お互いの投稿が見えなくなり、パーティやグループで
          一緒になることもなくなります。
        </Notice>
      </div>

      <div className={styles.addRow}>
        <label className={styles.addLabel} htmlFor="block-did">
          ブロックする相手の DID
        </label>
        <div className={styles.addControls}>
          <input
            id="block-did"
            className={styles.input}
            value={draft}
            placeholder="did:plc:..."
            disabled={adding}
            onChange={(event) => setDraft(event.target.value)}
          />
          <Button
            variant="primary"
            size="sm"
            onClick={() => void submit()}
            disabled={adding || draft.trim().length === 0}
          >
            {adding ? "追加中…" : "追加"}
          </Button>
        </div>
      </div>

      {message && (
        <p className={styles.message} role="alert">
          {message}
        </p>
      )}

      {state.status === "ok" ? (
        blocks.length > 0 ? (
          <section className={styles.section}>
            <Card padding="md">
              <List>
                {blocks.map((block) => (
                  <ListItem
                    key={block.did}
                    leading={<Avatar user={block} size={40} />}
                    title={block.name}
                    subtitle={`@${block.handle}`}
                    trailing={
                      <Button
                        variant="secondary"
                        size="sm"
                        disabled={removing.has(block.did)}
                        onClick={() => void remove(block)}
                      >
                        解除
                      </Button>
                    }
                  />
                ))}
              </List>
            </Card>
          </section>
        ) : (
          <BlocksState
            icon="block"
            title="ブロックした人はいません"
            description="ブロックすると、ここに表示され、あとから解除できます。"
          />
        )
      ) : state.status === "loading" ? (
        <BlocksSkeleton />
      ) : state.status === "error" ? (
        <BlocksState
          icon="cloud_off"
          title="ブロックリストを読み込めませんでした"
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
        <BlocksState
          icon="construction"
          title="ブロックは準備中です"
          description="この機能はまだ提供されていません。"
        />
      )}
    </AppShell>
  );
}

type BlocksStateProps = {
  icon: string;
  title: string;
  description: string;
  action?: ReactNode;
};

function BlocksState({ icon, title, description, action }: BlocksStateProps) {
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

function BlocksSkeleton() {
  return (
    <div className={styles.skeleton} aria-hidden>
      {[0, 1].map((row) => (
        <div key={row} className={styles.skeletonRow} />
      ))}
    </div>
  );
}
