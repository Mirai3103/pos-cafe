import { createFileRoute } from "@tanstack/react-router";
import { SettingsLayout } from "@/features/settings/components/settings-layout";
import { SETTINGS_CAPABILITIES } from "@/features/settings/lib/tabs";
import { requireAnyCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/settings")({
  beforeLoad: () => requireAnyCapability(SETTINGS_CAPABILITIES),
  component: () => <SettingsLayout />,
});
