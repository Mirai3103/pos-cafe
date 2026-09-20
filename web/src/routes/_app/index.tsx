import { createFileRoute } from "@tanstack/react-router";
import { PosTerminalView } from "@/features/pos/components/pos-terminal-view";

export const Route = createFileRoute("/_app/")({
  component: PosTerminalView,
});
