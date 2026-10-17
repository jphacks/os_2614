import { ProfileScreen } from "@/components/screens/ProfileScreen";
import { me, memories } from "@/data/mock";

export default function ProfilePage() {
  return <ProfileScreen me={me} memories={memories} />;
}
