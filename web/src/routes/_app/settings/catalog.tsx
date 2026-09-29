import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { CatalogView } from "@/features/catalog/components/catalog-view";
import { parseCatalogView, type CatalogViewKey } from "@/features/catalog/lib/views";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/settings/catalog")({
  validateSearch: (search: Record<string, unknown>): { view?: CatalogViewKey } => ({
    view: parseCatalogView(search.view),
  }),
  beforeLoad: () => requireCapability("catalog.administer_structure"),
  component: CatalogRoute,
});

function CatalogRoute() {
  const { view = "items" } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  return <CatalogView view={view} onViewChange={(next) => void navigate({ search: { view: next } })} />;
}
