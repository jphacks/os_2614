"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import type { GroupVisibility, User } from "@/domain/types";
import {
  InviteePicker,
  VisibilityOption,
} from "@/components/feature/group-create";
import { Icon } from "@/components/ui/Icon";
import { Notice } from "@/components/ui/Notice";
import { Slider } from "@/components/ui/Slider";
import styles from "./CreateGroupScreen.module.css";

const MAX_NAME = 30;

export type CreateGroupDraft = {
  name: string;
  visibility: GroupVisibility;
  approvalRatio: number;
  inviteeIds: string[];
  icon?: File;
};

export type CreateGroupScreenProps = {
  candidates: User[];
  areaLabel: string;
  onSubmit?: (draft: CreateGroupDraft) => void;
  onCancel?: () => void;
};

export function CreateGroupScreen({
  candidates,
  areaLabel,
  onSubmit,
  onCancel,
}: CreateGroupScreenProps) {
  const router = useRouter();
  const [name, setName] = useState("");
  const [visibility, setVisibility] = useState<GroupVisibility>("open");
  const [required, setRequired] = useState(
    Math.max(1, Math.ceil(candidates.length / 2)),
  );
  const [inviteeIds, setInviteeIds] = useState<string[]>([]);

  const total = Math.max(1, candidates.length);
  const percent = Math.round((required / total) * 100);

  const toggleInvitee = (id: string) => {
    setInviteeIds((prev) =>
      prev.includes(id) ? prev.filter((item) => item !== id) : [...prev, id],
    );
  };

  const submit = () => {
    if (name.trim().length === 0) {
      return;
    }
    onSubmit?.({
      name,
      visibility,
      approvalRatio: required / total,
      inviteeIds,
    });
    router.push("/friends/");
  };

  const cancel = () => {
    onCancel?.();
    router.back();
  };

  return (
    <div className={styles.screen}>
      <header className={styles.header}>
        <button type="button" className={styles.cancel} onClick={cancel}>
          キャンセル
        </button>
        <span className={styles.heading}>グループを作る</span>
        <button
          type="button"
          className={styles.submit}
          onClick={submit}
          disabled={name.trim().length === 0}
        >
          作成
        </button>
      </header>

      <div className={styles.nameSection}>
        <button type="button" className={styles.iconPicker} aria-label="グループアイコンを選ぶ">
          <Icon name="add_a_photo" size={28} />
        </button>
        <div className={styles.nameField}>
          <label className={styles.fieldLabel} htmlFor="group-name">
            グループ名
          </label>
          <input
            id="group-name"
            className={styles.input}
            value={name}
            maxLength={MAX_NAME}
            onChange={(event) => setName(event.target.value)}
          />
          <span className={styles.counter}>
            {name.length}/{MAX_NAME}
          </span>
        </div>
      </div>

      <div className={styles.band} />

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>公開範囲</h2>
        <div role="radiogroup" aria-label="公開範囲">
          <VisibilityOption
            icon="public"
            title="オープン"
            description="エリアにいる人がリストから自由に参加できます"
            selected={visibility === "open"}
            tone="notice"
            onSelect={() => setVisibility("open")}
          />
          <VisibilityOption
            icon="lock"
            title="クローズド（招待制）"
            description="招待された人が承諾するとグループができます"
            selected={visibility === "closed"}
            tone="accent"
            onSelect={() => setVisibility("closed")}
          />
        </div>
      </section>

      {visibility === "closed" && (
        <>
          <div className={styles.band} />
          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>承諾が必要な割合</h2>
            <p className={styles.sectionDesc}>
              招待した人のうち、この割合が承諾するとグループが作られます
            </p>
            <div className={styles.ratioRow}>
              <span className={styles.ratioValue}>{percent}%</span>
              <span className={styles.ratioNote}>
                {candidates.length}人中 {required}人が承諾すれば作成
              </span>
            </div>
            <Slider
              min={1}
              max={total}
              step={1}
              value={required}
              tone="accent"
              label="承諾が必要な割合"
              marks={["1人", "50%", "全員"]}
              onChange={setRequired}
            />
          </section>
        </>
      )}

      <div className={styles.band} />

      <section className={styles.section}>
        <div className={styles.sectionHead}>
          <h2 className={styles.sectionTitle}>招待する人</h2>
          <span className={styles.count}>{inviteeIds.length}人</span>
        </div>
        <InviteePicker
          candidates={candidates}
          selectedIds={inviteeIds}
          onToggle={toggleInvitee}
        />
      </section>

      <div className={styles.notice}>
        <Notice tone="info" icon="location_on">
          グループは今いるエリア（{areaLabel}）に作られます
        </Notice>
      </div>
    </div>
  );
}
