// web/src/features/catalog/lib/sellable-preview.ts
import type { CatalogSellableItemResponse } from "@/api/generated/models";
import type { Assignment, CatalogModel, CatItem } from "./catalog-model";
import { effectiveGroupIds } from "./item-inheritance";

/** The Batch Linker's unsaved state for one group. */
export type PreviewOverride = Assignment & { groupId: string };

function withOrWithout(ids: readonly string[], id: string, on: boolean): string[] {
  const rest = ids.filter((x) => x !== id);
  return on ? [...rest, id] : rest;
}

/**
 * Builds what the POS would receive for this item, so the preview renders the
 * real picker instead of a copy (spec §6). Unavailable sizes and options are
 * hidden, as on the POS.
 */
export function toSellablePreview(
  item: CatItem,
  model: CatalogModel,
  override?: PreviewOverride,
): CatalogSellableItemResponse {
  let inherited = model.categories.find((c) => c.id === item.categoryId)?.groupIds ?? [];
  let direct = item.directGroupIds;
  if (override) {
    inherited = withOrWithout(inherited, override.groupId, override.categoryIds.includes(item.categoryId));
    direct = withOrWithout(direct, override.groupId, override.itemIds.includes(item.id));
  }
  const groups = new Map(model.groups.map((g) => [g.id, g]));
  const modifierGroups = effectiveGroupIds(inherited, direct, item.excludedGroupIds).flatMap((id) => {
    const g = groups.get(id);
    if (!g) return [];
    return [
      {
        id: g.id,
        name: g.name,
        min_selections: g.min,
        max_selections: g.max,
        default_option_ids: g.defaultOptionIds,
        options: g.options.filter((o) => o.available).map((o) => ({ id: o.id, name: o.name, surcharge_vnd: o.surchargeVnd })),
      },
    ];
  });
  return {
    id: item.id,
    name: item.name,
    category_id: item.categoryId,
    code: item.code ?? undefined,
    image_url: item.imageUrl ?? undefined,
    price_vnd: item.priceVnd ?? undefined,
    sizes:
      item.priceVnd === null
        ? item.sizes.filter((s) => s.available).map((s) => ({ id: s.id, name: s.name, price_vnd: s.priceVnd }))
        : [],
    modifier_groups: modifierGroups,
  };
}
