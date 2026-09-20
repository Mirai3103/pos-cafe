import { createFileRoute } from "@tanstack/react-router";
import { TablesView } from "@/features/tables/components/tables-view";

export const Route = createFileRoute("/_app/tables")({
  component: TablesView,
});
