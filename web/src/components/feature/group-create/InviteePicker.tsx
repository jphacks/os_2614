"use client";

import type { ReactNode } from "react";
import type { User } from "@/domain/types";
import { Avatar } from "@/components/ui/Avatar";
import styles from "./InviteePicker.module.css";

function Invitee({
  name,
  selected,
  onSelect,
  children,
}: {
  name: string;
  selected?: boolean;
  onSelect?: () => void;
  children: ReactNode;
}) {
  return (
    <li className={styles.member}>
      <button
        type="button"
        className={styles.button}
        aria-pressed={selected}
        onClick={onSelect}
      >
        {children}
      </button>
      <span className={styles.name}>{name}</span>
    </li>
  );
}

export type InviteePickerProps = {
  candidates: User[];
  selectedIds: string[];
  onToggle: (id: string) => void;
  onAdd?: () => void;
};

export function InviteePicker({
  candidates,
  selectedIds,
  onToggle,
  onAdd,
}: InviteePickerProps) {
  return (
    <ul className={styles.strip}>
      {candidates.map((candidate) => {
        const selected = selectedIds.includes(candidate.id);
        return (
          <Invitee
            key={candidate.id}
            name={candidate.name}
            selected={selected}
            onSelect={() => onToggle(candidate.id)}
          >
            <Avatar user={candidate} size={48} ring={selected} />
          </Invitee>
        );
      })}
      <Invitee name="追加" onSelect={onAdd}>
        <Avatar user={{ name: "", tone: "blue" }} size={48} dashed />
      </Invitee>
    </ul>
  );
}
