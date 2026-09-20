import { createFileRoute } from "@tanstack/react-router";
import { HistoryView } from "@/features/history/components/history-view";

export const Route = createFileRoute("/_app/history")({
  component: HistoryView,
});
