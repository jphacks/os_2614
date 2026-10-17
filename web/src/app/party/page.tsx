import { PartyScreen } from "@/components/screens/PartyScreen";
import { me, party } from "@/data/mock";

export default function PartyPage() {
  return <PartyScreen party={party} me={me} />;
}
