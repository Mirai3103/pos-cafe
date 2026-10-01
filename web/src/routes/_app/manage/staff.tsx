import { createFileRoute } from "@tanstack/react-router";
import { StaffView } from "@/features/staff/components/staff-view";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/manage/staff")({
  beforeLoad: () => requireCapability("staff.administer"),
  component: StaffView,
});
