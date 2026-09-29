import { createFileRoute } from "@tanstack/react-router";
import { AvailabilityCounter, CatalogItemCounter } from "@/features/settings/components/availability-counter";
import { SettingsLayout } from "@/features/settings/components/settings-layout";
import { SETTINGS_CAPABILITIES } from "@/features/settings/lib/tabs";
import { StaffCounter } from "@/features/staff/components/staff-view";
import { requireAnyCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/settings")({
  beforeLoad: () => requireAnyCapability(SETTINGS_CAPABILITIES),
  component: () => (
    <SettingsLayout
      counters={{
        "/settings/availability": <AvailabilityCounter />,
        "/settings/catalog": <CatalogItemCounter />,
        "/settings/staff": <StaffCounter />,
      }}
    />
  ),
});
