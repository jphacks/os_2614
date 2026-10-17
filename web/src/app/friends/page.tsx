import { FriendsScreen } from "@/components/screens/FriendsScreen";
import { friends } from "@/data/mock";

export default function FriendsPage() {
  return <FriendsScreen friends={friends} />;
}
