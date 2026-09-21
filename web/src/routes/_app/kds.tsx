import { createFileRoute } from "@tanstack/react-router";
import { KdsView } from "@/features/kds/components/kds-view";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/kds")({
  beforeLoad: () => requireCapability("preparation.operate"),
  component: KdsView,
});
