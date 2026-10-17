import type { PartyMember } from "@/domain/types";
import { Avatar } from "@/components/ui/Avatar";
import { avatarToneFor } from "@/data/mappers";
import styles from "./MemberStrip.module.css";

export type MemberStripProps = {
  members: PartyMember[];
};

/** 表示名。displayName → handle → did の順で落とす。 */
function memberName(member: PartyMember): string {
  return member.displayName || member.handle || member.did;
}

export function MemberStrip({ members }: MemberStripProps) {
  // 在席・退出のどちらかを可視化する。退出者は末尾へ。
  const ordered = [...members].sort((a, b) => Number(a.left) - Number(b.left));

  return (
    <ul className={styles.strip}>
      {ordered.map((member) => {
        const name = memberName(member);
        const tone = avatarToneFor(member.did);
        return (
          <li
            key={member.did}
            className={[styles.member, member.left && styles.left]
              .filter(Boolean)
              .join(" ")}
          >
            <Avatar
              user={{ name, avatarUrl: member.avatarUrl, tone }}
              size={48}
              online={member.present && !member.left}
            />
            <span className={styles.name}>{name}</span>
            {member.left && <span className={styles.status}>退出済み</span>}
          </li>
        );
      })}
    </ul>
  );
}
