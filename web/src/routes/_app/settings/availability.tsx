import { createFileRoute } from "@tanstack/react-router";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/settings/availability")({
  beforeLoad: () => requireCapability("catalog.manage_availability"),
  component: () => null,
});
