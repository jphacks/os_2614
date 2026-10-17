import type { User } from "@/domain/types";
import { Avatar } from "@/components/ui/Avatar";
import styles from "./MemberStrip.module.css";

export type PartyMember = User & { viaFriend: boolean };

export type MemberStripProps = {
  members: PartyMember[];
};

export function MemberStrip({ members }: MemberStripProps) {
  return (
    <ul className={styles.strip}>
      {members.map((member) => (
        <li key={member.id} className={styles.member}>
          <Avatar user={member} size={48} dashed={member.viaFriend} />
          <span className={styles.name}>{member.name}</span>
        </li>
      ))}
    </ul>
  );
}
