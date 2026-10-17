"use client";

import type { Memory, Profile } from "@/domain/types";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { MemoryCard } from "@/components/feature/memory";
import { SectionHeader } from "@/components/ui/SectionHeader";
import { StatTriple } from "@/components/ui/StatTriple";
import styles from "./MemoryScreen.module.css";

export type MemoryScreenProps = {
  me: Profile;
  memories: Memory[];
};

export function MemoryScreen({ me, memories }: MemoryScreenProps) {
  return (
    <AppShell tab="profile">
      <ScreenHeader
        back
        title="メモリー"
        subtitle="終わったパーティは、ここに残ります"
      />

      <div className={styles.content}>
        <StatTriple
          items={[
            { value: me.counts.encounters, label: "すれ違い" },
            { value: me.counts.friends, label: "友達" },
            { value: me.counts.parties, label: "パーティ" },
          ]}
        />

        <section className={styles.section}>
          <SectionHeader title="アーカイブ" action={{ label: "新しい順" }} />
          <div className={styles.list}>
            {memories.map((memory) => (
              <MemoryCard key={memory.id} memory={memory} />
            ))}
          </div>
        </section>
      </div>
    </AppShell>
  );
}
