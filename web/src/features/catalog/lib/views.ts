export const CATALOG_VIEWS = ["items", "categories", "groups", "linker"] as const;

export type CatalogViewKey = (typeof CATALOG_VIEWS)[number];

export function parseCatalogView(value: unknown): CatalogViewKey {
  return CATALOG_VIEWS.includes(value as CatalogViewKey) ? (value as CatalogViewKey) : "items";
}
