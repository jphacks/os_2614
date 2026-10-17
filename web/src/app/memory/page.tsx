import { MemoryScreen } from "@/components/screens/MemoryScreen";
import { me, memories } from "@/data/mock";

export default function MemoryPage() {
  return <MemoryScreen me={me} memories={memories} />;
}
