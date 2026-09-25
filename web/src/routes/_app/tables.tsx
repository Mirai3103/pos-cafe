import { createFileRoute } from "@tanstack/react-router";
import { TablesView } from "@/features/tables/components/tables-view";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/tables")({
  beforeLoad: () => requireCapability("sales.operate"),
  component: TablesView,
});
