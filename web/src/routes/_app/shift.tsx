import { createFileRoute } from "@tanstack/react-router";
import { ShiftView } from "@/features/shift/components/shift-view";

export const Route = createFileRoute("/_app/shift")({
  component: ShiftView,
});
