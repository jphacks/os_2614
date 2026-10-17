"use client";

import { useCallback, useState } from "react";
import type { GroupInvite, GroupInviteInboxItem } from "@/domain/types";
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
import { Tag } from "@/components/ui/Tag";
import styles from "./GroupInvitesScreen.module.css";

/** 招待操作の結果。unavailable はエンドポイント未対応 (404・405・501)。 */
export type GroupInviteActionResult =
  | { status: "ok" }
  | { status: "unavailable" }
  | { status: "error"; message: string };

export type GroupInvitesScreenProps = {
  /** 対象の closed グループ (クエリ `?id=`)。空なら自分宛ての招待だけを出す。 */
  groupId: string;
  /** GET /groups/{id}/invites (09 §6)。groupId が無いときは undefined。 */
  members?: ResourceState<GroupInvite[]>;
  /** GET /group-invites (09 §6) の取得状態。 */
  inbox: ResourceState<GroupInviteInboxItem[]>;
  /** POST /groups/{id}/invites {did}。 */
  onCreateInvite: (did: string) => Promise<GroupInviteActionResult>;
  /** POST /groups/{id}/invites/{inviteId}/approve。 */
  onApprove: (inviteId: string) => Promise<GroupInviteActionResult>;
  /** DELETE /groups/{id}/invites/{inviteId} (招待者が collecting の間だけ)。 */
  onCancel: (inviteId: string) => Promise<GroupInviteActionResult>;
  /** POST /group-invites/{inviteId}/accept。 */
  onAccept: (inviteId: string) => Promise<GroupInviteActionResult>;
  /** POST /group-invites/{inviteId}/decline。 */
  onDecline: (inviteId: string) => Promise<GroupInviteActionResult>;
  /** 取得に失敗したときの再読み込み。 */
  onRetry: () => void;
};

/** 招待の状態 (09 §6)。 */
const STATUS_LABEL: Record<string, string> = {
  collecting: "承認待ち",
  offered: "提示済み",
  accepted: "参加済み",
  declined: "辞退",
  expired: "期限切れ",
};

function statusLabel(status: string): string {
  return STATUS_LABEL[status] ?? status;
}

export function GroupInvitesScreen({
  groupId,
  members,
  inbox,
  onCreateInvite,
  onApprove,
  onCancel,
  onAccept,
  onDecline,
  onRetry,
}: GroupInvitesScreenProps) {
  const [draft, setDraft] = useState("");
  const [creating, setCreating] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);

  const create = useCallback(async () => {
    const did = draft.trim();
    if (!did || creating) {
      return;
    }
    setMessage(null);
    setCreating(true);
    const result = await onCreateInvite(did);
    setCreating(false);
    if (result.status === "ok") {
      setDraft("");
      return;
    }
    setMessage(
      result.status === "unavailable"
        ? "招待はまだ利用できません。"
        : result.message,
    );
  }, [creating, draft, onCreateInvite]);

  const run = useCallback(
    async (id: string, action: (id: string) => Promise<GroupInviteActionResult>) => {
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
      <ScreenHeader
        title="グループの招待"
        back={groupId ? `/group/?id=${groupId}` : "/"}
        subtitle={groupId ? undefined : "自分宛てに届いた招待"}
      />

      {message && (
        <p className={styles.message} role="status">
          {message}
        </p>
      )}

      {groupId && members && (
        <section className={styles.section}>
          <SectionHeader title="招待を集めている" />
          <Card padding="md">
            <label className={styles.fieldLabel} htmlFor="invite-did">
              招待する相手の DID
            </label>
            <div className={styles.fieldRow}>
              <input
                id="invite-did"
                className={styles.input}
                value={draft}
                placeholder="did:plc:..."
                disabled={creating}
                onChange={(event) => setDraft(event.target.value)}
              />
              <Button
                variant="primary"
                onClick={() => void create()}
                disabled={creating || draft.trim().length === 0}
              >
                {creating ? "招待中…" : "招待する"}
              </Button>
            </div>
            <p className={styles.hint}>
              招待できるのは、あなたの友達か、すれ違ったことのある相手だけです。
            </p>
          </Card>

          {members.status === "ok" ? (
            members.data.length > 0 ? (
              <Card padding="md">
                <List>
                  {members.data.map((invite) => (
                    <ListItem
                      key={invite.id}
                      leading={
                        <span className={styles.avatar} aria-hidden>
                          <Icon name="person" size={22} />
                        </span>
                      }
                      title={invite.targetDid}
                      subtitle={`${statusLabel(invite.status)}・承認 ${invite.approvedCount}/${invite.requiredCount}`}
                      trailing={
                        invite.status === "collecting" ? (
                          <span className={styles.actions}>
                            <Button
                              variant="primary"
                              size="sm"
                              disabled={busyId === invite.id}
                              onClick={() => void run(invite.id, onApprove)}
                            >
                              承認
                            </Button>
                            <Button
                              variant="secondary"
                              size="sm"
                              disabled={busyId === invite.id}
                              onClick={() => void run(invite.id, onCancel)}
                            >
                              取り消す
                            </Button>
                          </span>
                        ) : (
                          <Tag label={statusLabel(invite.status)} />
                        )
                      }
                    />
                  ))}
                </List>
              </Card>
            ) : (
              <Card padding="md">
                <p className={styles.empty}>承認待ちの招待はありません</p>
              </Card>
            )
          ) : (
            <MembersState state={members} onRetry={onRetry} />
          )}
        </section>
      )}

      <section className={styles.section}>
        <SectionHeader title="あなたへの招待" />
        {inbox.status === "ok" ? (
          inbox.data.length > 0 ? (
            <Card padding="md">
              <List>
                {inbox.data.map((invite) => (
                  <ListItem
                    key={invite.id}
                    leading={
                      invite.inviter ? (
                        <Avatar user={invite.inviter} size={40} />
                      ) : (
                        <span className={styles.avatar} aria-hidden>
                          <Icon name="group" size={22} />
                        </span>
                      )
                    }
                    title={invite.groupName}
                    subtitle={
                      invite.inviter
                        ? `${invite.inviter.name}さんからの招待`
                        : "グループへの招待"
                    }
                    trailing={
                      <span className={styles.actions}>
                        <Button
                          variant="primary"
                          size="sm"
                          disabled={busyId === invite.id}
                          onClick={() => void run(invite.id, onAccept)}
                        >
                          参加
                        </Button>
                        <Button
                          variant="secondary"
                          size="sm"
                          disabled={busyId === invite.id}
                          onClick={() => void run(invite.id, onDecline)}
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
              <p className={styles.empty}>届いている招待はありません</p>
            </Card>
          )
        ) : (
          <MembersState state={inbox} onRetry={onRetry} label="あなたへの招待" />
        )}
      </section>

      <div className={styles.footer}>
        <Notice tone="info" icon="info">
          招待は、必要な人数の承認が集まると「提示済み」になり、相手が承諾すると参加できます。
        </Notice>
      </div>
    </AppShell>
  );
}

type MembersStateProps<T> = {
  state: ResourceState<T[]>;
  onRetry: () => void;
  label?: string;
};

function MembersState<T>({ state, onRetry, label = "招待" }: MembersStateProps<T>) {
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
        <Button variant="secondary" icon="refresh" onClick={onRetry}>
          再読み込み
        </Button>
      </div>
    );
  }
  return (
    <Card padding="md">
      <p className={styles.empty}>{label}は準備中です</p>
    </Card>
  );
}
