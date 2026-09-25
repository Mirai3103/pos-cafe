import { createFileRoute } from "@tanstack/react-router";
import { AvailabilityView } from "@/features/settings/components/availability-view";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/settings/availability")({
  beforeLoad: () => requireCapability("catalog.manage_availability"),
  component: AvailabilityView,
});
