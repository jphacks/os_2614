"use client";

import { useCallback, useState } from "react";
import type { FriendRequest, Invite } from "@/domain/types";
import type { ResourceState } from "@/data/hooks";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { List, ListItem } from "@/components/ui/List";
import { Notice } from "@/components/ui/Notice";
import { SectionHeader } from "@/components/ui/SectionHeader";
import styles from "./FriendRequestsScreen.module.css";

/** 申請 / 招待の操作結果。unavailable はエンドポイント未対応 (404・405・501)。 */
export type FriendActionResult =
  | { status: "ok" }
  | { status: "unavailable" }
  | { status: "error"; message: string };

/** 招待コードの発行結果。 */
export type InviteCreateResult =
  | { status: "ok"; invite: Invite }
  | { status: "unavailable" }
  | { status: "error"; message: string };

export type FriendRequestsScreenProps = {
  /** GET /friends/requests?box=incoming (07 §2.2) の取得状態。 */
  incoming: ResourceState<FriendRequest[]>;
  /** GET /friends/requests?box=outgoing (07 §2.2) の取得状態。 */
  outgoing: ResourceState<FriendRequest[]>;
  /** POST /friends/requests/{id}/accept。 */
  onAccept: (id: string) => Promise<FriendActionResult>;
  /** POST /friends/requests/{id}/decline。 */
  onDecline: (id: string) => Promise<FriendActionResult>;
  /** DELETE /friends/requests/{id} (送信者が取り消す)。 */
  onCancel: (id: string) => Promise<FriendActionResult>;
  /** POST /friends/invites → 招待コードを発行する。 */
  onCreateInvite: () => Promise<InviteCreateResult>;
  /** POST /friends/invites/{code}/accept → コードを読み取って友達になる。 */
  onAcceptInvite: (code: string) => Promise<FriendActionResult>;
  /** 取得に失敗したときの再読み込み。 */
  onRetry?: () => void;
};

/** via の表示ラベル (07 §2)。 */
const VIA_LABEL: Record<string, string> = {
  encounter: "すれ違いから",
  space: "同じグループから",
  invite: "招待コードから",
  realtime: "いま届きました",
};

function viaLabel(via: string): string {
  return VIA_LABEL[via] ?? via;
}

export function FriendRequestsScreen({
  incoming,
  outgoing,
  onAccept,
  onDecline,
  onCancel,
  onCreateInvite,
  onAcceptInvite,
  onRetry,
}: FriendRequestsScreenProps) {
  const [invite, setInvite] = useState<Invite | null>(null);
  const [creating, setCreating] = useState(false);
  const [code, setCode] = useState("");
  const [acceptingCode, setAcceptingCode] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);

  const createInvite = useCallback(async () => {
    if (creating) {
      return;
    }
    setMessage(null);
    setCreating(true);
    const result = await onCreateInvite();
    setCreating(false);
    if (result.status === "ok") {
      setInvite(result.invite);
      return;
    }
    setMessage(
      result.status === "unavailable"
        ? "招待コードはまだ利用できません。"
        : result.message,
    );
  }, [creating, onCreateInvite]);

  const acceptCode = useCallback(async () => {
    const trimmed = code.trim();
    if (!trimmed || acceptingCode) {
      return;
    }
    setMessage(null);
    setAcceptingCode(true);
    const result = await onAcceptInvite(trimmed);
    setAcceptingCode(false);
    if (result.status === "ok") {
      setCode("");
      setMessage("友達になりました。");
      return;
    }
    setMessage(
      result.status === "unavailable"
        ? "招待コードはまだ利用できません。"
        : result.message,
    );
  }, [acceptingCode, code, onAcceptInvite]);

  const run = useCallback(
    async (id: string, action: (id: string) => Promise<FriendActionResult>) => {
      if (busyId !== null) {
        return;
      }
      setMessage(null);
      setBusyId(id);
      const result = await action(id);
      setBusyId(null);
      if (result.status === "ok") {
        return;
      }
      setMessage(
        result.status === "unavailable"
          ? "この操作はまだ利用できません。"
          : result.message,
      );
    },
    [busyId],
  );

  return (
    <AppShell>
      <ScreenHeader title="友達申請" back="/friends/" />

      <section className={styles.section}>
        <SectionHeader title="招待コード" />
        <Card padding="md">
          <p className={styles.note}>
            対面でこのコードを渡すと、その場で友達になれます（10 分間・1 回だけ）。
          </p>
          <p className={styles.note}>
            コードを作ると、読み取った人を Bluesky でもフォローします（公開されます）。
          </p>
          {invite ? (
            <div className={styles.inviteBox}>
              <span className={styles.inviteIcon} aria-hidden>
                <Icon name="qr_code_2" size={40} />
              </span>
              <p className={styles.inviteCode} aria-label="招待コード">
                {invite.code}
              </p>
              <p className={styles.inviteMeta}>{invite.url}</p>
              <p className={styles.inviteMeta}>
                {new Date(invite.expiresAt).toLocaleTimeString("ja-JP", {
                  hour: "2-digit",
                  minute: "2-digit",
                })}
                まで有効
              </p>
            </div>
          ) : (
            <Button
              variant="primary"
              icon="qr_code_2"
              onClick={() => void createInvite()}
              disabled={creating}
            >
              {creating ? "作成中…" : "コードを作る"}
            </Button>
          )}
        </Card>

        <Card padding="md">
          <label className={styles.fieldLabel} htmlFor="invite-code">
            コードを受け取った
          </label>
          <div className={styles.fieldRow}>
            <input
              id="invite-code"
              className={styles.input}
              value={code}
              placeholder="7KQ2-M9XD"
              disabled={acceptingCode}
              onChange={(event) => setCode(event.target.value)}
            />
            <Button
              variant="secondary"
              onClick={() => void acceptCode()}
              disabled={acceptingCode || code.trim().length === 0}
            >
              {acceptingCode ? "追加中…" : "追加"}
            </Button>
          </div>
        </Card>
      </section>

      {message && (
        <p className={styles.message} role="status">
          {message}
        </p>
      )}

      <section className={styles.section}>
        <p className={styles.note}>
          承認すると、Bluesky でもフォローを返します（公開されます）。
        </p>
        <SectionHeader
          title="受け取った申請"
          action={
            incoming.status === "ok" && incoming.data.length > 0
              ? { label: `${incoming.data.length}件` }
              : undefined
          }
        />
        {incoming.status === "ok" ? (
          incoming.data.length > 0 ? (
            <Card padding="md">
              <List>
                {incoming.data.map((request) => (
                  <ListItem
                    key={request.id}
                    leading={<Avatar user={request.peer} size={40} />}
                    title={request.peer.name}
                    subtitle={`@${request.peer.handle}・${viaLabel(request.via)}`}
                    trailing={
                      <span className={styles.actions}>
                        <Button
                          variant="primary"
                          size="sm"
                          disabled={busyId === request.id}
                          onClick={() => void run(request.id, onAccept)}
                        >
                          承認
                        </Button>
                        <Button
                          variant="secondary"
                          size="sm"
                          disabled={busyId === request.id}
                          onClick={() => void run(request.id, onDecline)}
                        >
                          断る
                        </Button>
                      </span>
                    }
                  />
                ))}
              </List>
            </Card>
          ) : (
            <Card padding="md">
              <p className={styles.empty}>申請は届いていません</p>
            </Card>
          )
        ) : (
          <RequestState state={incoming} onRetry={onRetry} label="受け取った申請" />
        )}
      </section>

      <section className={styles.section}>
        <SectionHeader title="送った申請" />
        {outgoing.status === "ok" ? (
          outgoing.data.length > 0 ? (
            <Card padding="md">
              <List>
                {outgoing.data.map((request) => (
                  <ListItem
                    key={request.id}
                    leading={<Avatar user={request.peer} size={40} />}
                    title={request.peer.name}
                    subtitle={`@${request.peer.handle}・${viaLabel(request.via)}`}
                    trailing={
                      <Button
                        variant="secondary"
                        size="sm"
                        disabled={busyId === request.id}
                        onClick={() => void run(request.id, onCancel)}
                      >
                        取り消す
                      </Button>
                    }
                  />
                ))}
              </List>
            </Card>
          ) : (
            <Card padding="md">
              <p className={styles.empty}>送った申請はありません</p>
            </Card>
          )
        ) : (
          <RequestState state={outgoing} onRetry={onRetry} label="送った申請" />
        )}
      </section>

      <div className={styles.footer}>
        <Notice tone="info" icon="info">
          検索はありません。すれ違い・同じグループ・招待コードで会った人にだけ申請できます。申請はその人を Bluesky でフォローすることで、相手がフォローを返すと友達になります。
        </Notice>
      </div>
    </AppShell>
  );
}

type RequestStateProps = {
  state: ResourceState<FriendRequest[]>;
  onRetry?: () => void;
  label: string;
};

function RequestState({ state, onRetry, label }: RequestStateProps) {
  if (state.status === "loading") {
    return (
      <Card padding="md">
        <p className={styles.empty}>読み込み中…</p>
      </Card>
    );
  }
  if (state.status === "error") {
    return (
      <div className={styles.state}>
        <span className={styles.stateIcon} aria-hidden>
          <Icon name="cloud_off" size={28} />
        </span>
        <p className={styles.stateTitle}>{label}を読み込めませんでした</p>
        {onRetry && (
          <Button variant="secondary" icon="refresh" onClick={onRetry}>
            再読み込み
          </Button>
        )}
      </div>
    );
  }
  return (
    <Card padding="md">
      <p className={styles.empty}>{label}は準備中です</p>
    </Card>
  );
}
