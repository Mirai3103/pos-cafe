// web/src/features/catalog/lib/linker.ts
import { sameSet, type Assignment, type CatalogModel, type CatItem } from "./catalog-model";

export type LinkState = "excluded" | "direct" | "inherited" | "none";

/**
 * An item excluding the group cannot be assigned directly (INVALID_INHERITANCE,
 * BA-1 §4.3); the Manager lifts the exclusion in the item form first.
 */
export function linkState(item: CatItem, groupId: string, draft: Assignment): LinkState {
  if (item.excludedGroupIds.includes(groupId)) return "excluded";
  if (draft.itemIds.includes(item.id)) return "direct";
  if (draft.categoryIds.includes(item.categoryId)) return "inherited";
  return "none";
}

function toggle(ids: readonly string[], id: string): string[] {
  return ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id];
}

export function toggleItem(draft: Assignment, itemId: string): Assignment {
  return { ...draft, itemIds: toggle(draft.itemIds, itemId) };
}

export function toggleCategory(draft: Assignment, categoryId: string): Assignment {
  return { ...draft, categoryIds: toggle(draft.categoryIds, categoryId) };
}

/** "Chọn hết": ticks every item of the category that does not exclude the group. */
export function selectAllInCategory(draft: Assignment, model: CatalogModel, groupId: string, categoryId: string): Assignment {
  const add = model.items
    .filter((i) => i.categoryId === categoryId && !i.excludedGroupIds.includes(groupId) && !draft.itemIds.includes(i.id))
    .map((i) => i.id);
  return { ...draft, itemIds: [...draft.itemIds, ...add] };
}

/** "Bỏ chọn hết": unticks every item of the category. */
export function clearCategoryItems(draft: Assignment, model: CatalogModel, categoryId: string): Assignment {
  const inCategory = new Set(model.items.filter((i) => i.categoryId === categoryId).map((i) => i.id));
  return { ...draft, itemIds: draft.itemIds.filter((id) => !inCategory.has(id)) };
}

export function sameAssignment(a: Assignment, b: Assignment): boolean {
  return sameSet(a.itemIds, b.itemIds) && sameSet(a.categoryIds, b.categoryIds);
}
