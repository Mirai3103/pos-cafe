import { createFileRoute } from "@tanstack/react-router";
import { ShiftView } from "@/features/shift/components/shift-view";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/shift")({
  beforeLoad: () => requireCapability("sales_shift.operate"),
  component: ShiftView,
});
