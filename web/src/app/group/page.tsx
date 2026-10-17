import { GroupScreen } from "@/components/screens/GroupScreen";
import { groupInfoKeisanden, posts } from "@/data/mock";

export default function GroupPage() {
  return <GroupScreen group={groupInfoKeisanden} posts={posts} />;
}
