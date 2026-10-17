"use client";

import type { Memory, Profile } from "@/domain/types";
import { AppShell } from "@/components/layout/AppShell";
import { MemoryThumbCard } from "@/components/feature/memory";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Icon } from "@/components/ui/Icon";
import { IconButton } from "@/components/ui/IconButton";
import { List, ListItem } from "@/components/ui/List";
import { SectionHeader } from "@/components/ui/SectionHeader";
import { StatTriple } from "@/components/ui/StatTriple";
import styles from "./ProfileScreen.module.css";

export type ProfileScreenProps = {
  me: Profile;
  memories: Memory[];
  onEditAvatar?: () => void;
  onEditProfile?: () => void;
};

export function ProfileScreen({
  me,
  memories,
  onEditAvatar,
  onEditProfile,
}: ProfileScreenProps) {
  return (
    <AppShell tab="profile">
      <div className={styles.content}>
        <div className={styles.bannerWrap}>
          <div className={styles.banner} aria-hidden>
            <span className={`${styles.cloud} ${styles.cloudA}`} />
            <span className={`${styles.cloud} ${styles.cloudB}`} />
            <span className={`${styles.cloud} ${styles.cloudC}`} />
          </div>
          <div className={styles.bannerEdit}>
            <IconButton
              icon="edit"
              label="バナーを編集"
              variant="surface"
              onClick={onEditAvatar}
            />
          </div>
          <span className={styles.avatar}>
            <Avatar user={me} size={80} />
          </span>
          <div className={styles.editButton}>
            <Button variant="secondary" size="sm" icon="edit" onClick={onEditProfile}>
              編集
            </Button>
          </div>
        </div>

        <div className={styles.nameRow}>
          <h1 className={styles.name}>{me.name}</h1>
          {me.status && (
            <span className={styles.status}>
              <span className={styles.onlineDot} aria-hidden />
              {me.status}
            </span>
          )}
        </div>
        <p className={styles.bio}>{me.bio}</p>

        <StatTriple
          items={[
            { value: me.counts.encounters, label: "すれ違い" },
            { value: me.counts.friends, label: "友達" },
            { value: me.counts.parties, label: "パーティ" },
          ]}
        />

        <section className={styles.section}>
          <SectionHeader
            title="メモリー"
            action={{ label: "すべて見る ›", href: "/memory/" }}
          />
          <div className={styles.memories}>
            {memories.map((memory) => (
              <MemoryThumbCard key={memory.id} memory={memory} />
            ))}
          </div>
        </section>

        <Card padding="md">
          <List>
            <ListItem
              leading={<Icon name="tune" size={22} />}
              title="設定"
              href="/settings/"
              trailing={<Icon name="chevron_right" size={20} />}
            />
            <ListItem
              leading={<Icon name="insights" size={22} />}
              title="これまでの統計"
              href="/stats/"
              trailing={<Icon name="chevron_right" size={20} />}
            />
          </List>
        </Card>
      </div>
    </AppShell>
  );
}
