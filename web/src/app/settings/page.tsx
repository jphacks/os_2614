import { SettingsScreen } from "@/components/screens/SettingsScreen";
import { settings } from "@/data/mock";

export default function SettingsPage() {
  return <SettingsScreen settings={settings} />;
}
