import { createFileRoute } from "@tanstack/react-router";
import { SettingsView } from "@/features/settings/components/settings-view";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/settings")({
  beforeLoad: () => requireCapability("staff.administer"),
  component: SettingsView,
});
