import { createFileRoute } from "@tanstack/react-router";
import { AvailabilityView } from "@/features/manage/components/availability-view";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/manage/availability")({
  beforeLoad: () => requireCapability("catalog.manage_availability"),
  component: AvailabilityView,
});
