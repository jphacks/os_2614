import { CreateGroupScreen } from "@/components/screens/CreateGroupScreen";
import { users } from "@/data/mock";

export default function CreateGroupPage() {
  return <CreateGroupScreen candidates={users} areaLabel="大学キャンパス" />;
}
