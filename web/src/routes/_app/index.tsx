import { createFileRoute } from "@tanstack/react-router";
import { PosTerminalView } from "@/features/pos/components/pos-terminal-view";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/")({
  beforeLoad: () => requireCapability("sales.operate"),
  component: PosTerminalView,
});
