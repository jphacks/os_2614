"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import type { Party, User } from "@/domain/types";
import { AppShell } from "@/components/layout/AppShell";
import { MemberStrip, PartyHero, PhotoGrid } from "@/components/feature/party";
import { Card } from "@/components/ui/Card";
import { Fab } from "@/components/ui/Fab";
import { Icon } from "@/components/ui/Icon";
import { Toggle } from "@/components/ui/Toggle";
import styles from "./PartyScreen.module.css";

export type PartyScreenProps = {
  party: Party | null;
  me: User;
  onToggleFixed?: (fixed: boolean) => void;
  onCompose?: (party: Party) => void;
};

export function PartyScreen({
  party,
  me,
  onToggleFixed,
  onCompose,
}: PartyScreenProps) {
  const router = useRouter();
  const [fixed, setFixed] = useState(party?.fixed ?? false);

  if (!party) {
    return (
      <AppShell tab="party">
        <div className={styles.empty}>
          <p>近くのフレンドと会うとパーティが始まります</p>
        </div>
      </AppShell>
    );
  }

  const companions = party.members
    .filter((member) => member.id !== me.id)
    .map((member) => member.name)
    .join("・");

  const handleFixed = (next: boolean) => {
    setFixed(next);
    onToggleFixed?.(next);
  };

  const handleCompose = () => {
    onCompose?.(party);
    router.push(`/compose/?target=party:${party.id}`);
  };

  return (
    <AppShell tab="party">
      <header className={styles.header}>
        <h1 className={styles.title}>パーティ</h1>
        <p className={styles.subtitle}>{companions} と合流しました</p>
      </header>

      <div className={styles.gutter}>
        <PartyHero party={party} />
      </div>

      <div className={styles.gutter}>
        <Card padding="md">
          <div className={styles.fixedRow}>
            <Icon name="push_pin" size={22} />
            <div className={styles.fixedText}>
              <span className={styles.fixedTitle}>パーティを固定</span>
              <span className={styles.fixedNote}>
                旅行のときなど、はぐれても解散しません
              </span>
            </div>
            <Toggle
              checked={fixed}
              onChange={handleFixed}
              label="パーティを固定"
            />
          </div>
        </Card>
      </div>

      <div className={styles.section}>
        <div className={styles.sectionHead}>
          <h2 className={styles.sectionTitle}>メンバー {party.members.length}</h2>
          <span className={styles.hint}>点線 ＝ フレンドのフレンド</span>
        </div>
      </div>
      <div className={styles.members}>
        <MemberStrip members={party.members} />
      </div>

      <div className={styles.section}>
        <div className={styles.sectionHead}>
          <h2 className={styles.sectionTitle}>共有された写真</h2>
          <span className={styles.hint}>{party.photoCount}枚</span>
        </div>
      </div>
      <div className={styles.gutter}>
        <PhotoGrid photos={party.photos} photoCount={party.photoCount} />
      </div>

      <Fab label="投稿する" onClick={handleCompose} />
    </AppShell>
  );
}
