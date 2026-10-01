import { createFileRoute } from "@tanstack/react-router";
import { AvailabilityCounter, CatalogItemCounter } from "@/features/manage/components/availability-counter";
import { ManageLayout } from "@/features/manage/components/manage-layout";
import { MANAGE_CAPABILITIES } from "@/features/manage/lib/tabs";
import { StaffCounter } from "@/features/staff/components/staff-view";
import { requireAnyCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/manage")({
  beforeLoad: () => requireAnyCapability(MANAGE_CAPABILITIES),
  component: () => (
    <ManageLayout
      counters={{
        "/manage/availability": <AvailabilityCounter />,
        "/manage/catalog": <CatalogItemCounter />,
        "/manage/staff": <StaffCounter />,
      }}
    />
  ),
});
