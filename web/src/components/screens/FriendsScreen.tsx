"use client";

import type { Friend } from "@/domain/types";
import { AppShell } from "@/components/layout/AppShell";
import { ScreenHeader } from "@/components/layout/ScreenHeader";
import { FriendRow } from "@/components/feature/friend";
import { Card } from "@/components/ui/Card";
import { IconButton } from "@/components/ui/IconButton";
import { List } from "@/components/ui/List";
import { SectionHeader } from "@/components/ui/SectionHeader";
import { useNow } from "@/hooks/useNow";
import styles from "./FriendsScreen.module.css";

export type FriendsScreenProps = {
  friends: Friend[];
  onAddFriend?: () => void;
};

export function FriendsScreen({ friends, onAddFriend }: FriendsScreenProps) {
  const tick = useNow(60_000);
  const now = tick > 0 ? tick : null;
  const together = friends.filter((friend) => friend.together);
  const others = friends.filter((friend) => !friend.together);

  return (
    <AppShell tab="friends">
      <ScreenHeader
        title="友達"
        count={`${friends.length}人`}
        subtitle="位置はエリア名だけ共有しています"
        trailing={
          <IconButton
            icon="person_add"
            label="友達を追加"
            variant="filled"
            onClick={onAddFriend}
          />
        }
      />

      <section className={styles.section}>
        <SectionHeader title="いっしょにいる" />
        <Card padding="md">
          <List>
            {together.map((friend) => (
              <FriendRow key={friend.id} friend={friend} now={now} />
            ))}
          </List>
        </Card>
      </section>

      <section className={styles.section}>
        <SectionHeader title="みんな" action={{ label: "近い順" }} />
        <Card padding="md">
          <List>
            {others.map((friend) => (
              <FriendRow key={friend.id} friend={friend} now={now} />
            ))}
          </List>
        </Card>
      </section>

      <p className={styles.footer}>
        検索はありません。すれ違ったことのある人とだけ友達になれます
      </p>
    </AppShell>
  );
}
