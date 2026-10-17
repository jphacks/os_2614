import { Suspense } from "react";
import { GroupReplyScreen } from "@/components/screens/GroupReplyScreen";
import { groupInfoKeisanden, me, posts, replies } from "@/data/mock";

export default function GroupRepliesPage() {
  return (
    <Suspense fallback={null}>
      <GroupReplyScreen
        group={groupInfoKeisanden}
        posts={posts}
        replies={replies}
        me={me}
      />
    </Suspense>
  );
}
