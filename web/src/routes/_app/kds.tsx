import { createFileRoute } from "@tanstack/react-router";
import { KdsView } from "@/features/kds/components/kds-view";

export const Route = createFileRoute("/_app/kds")({
  component: KdsView,
});
