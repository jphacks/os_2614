import { StatsScreen } from "@/components/screens/StatsScreen";
import { stats } from "@/data/mock";

export default function StatsPage() {
  return <StatsScreen stats={stats} />;
}
