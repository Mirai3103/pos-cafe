import { createFileRoute } from "@tanstack/react-router";
import { PosView } from "@/features/pos/components/pos-view";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/")({
  beforeLoad: () => requireCapability("sales.operate"),
  component: PosView,
});
