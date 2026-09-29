// web/src/features/catalog/lib/item-inheritance.ts
import type { CatCategory } from "./catalog-model";
import type { ItemForm } from "./forms";

export function inheritedGroupIds(categories: readonly CatCategory[], categoryId: string): string[] {
  return categories.find((c) => c.id === categoryId)?.groupIds ?? [];
}

/** What the POS offers: inherited minus excluded, then direct groups not already present. */
export function effectiveGroupIds(
  inherited: readonly string[],
  direct: readonly string[],
  excluded: readonly string[],
): string[] {
  const out = inherited.filter((id) => !excluded.includes(id));
  for (const id of direct) if (!out.includes(id)) out.push(id);
  return out;
}

/** ADR-059: an exclusion survives only while the new category still provides the group. */
export function changeCategory(form: ItemForm, categories: readonly CatCategory[], categoryId: string): ItemForm {
  const inherited = inheritedGroupIds(categories, categoryId);
  return { ...form, categoryId, excludedGroupIds: form.excludedGroupIds.filter((id) => inherited.includes(id)) };
}

/** Excluding also detaches, because a group cannot be both direct and excluded (INVALID_INHERITANCE). */
export function toggleExcluded(form: ItemForm, groupId: string): ItemForm {
  if (form.excludedGroupIds.includes(groupId)) {
    return { ...form, excludedGroupIds: form.excludedGroupIds.filter((id) => id !== groupId) };
  }
  return {
    ...form,
    excludedGroupIds: [...form.excludedGroupIds, groupId],
    directGroupIds: form.directGroupIds.filter((id) => id !== groupId),
  };
}

export function toggleDirect(form: ItemForm, groupId: string): ItemForm {
  const on = form.directGroupIds.includes(groupId);
  return {
    ...form,
    directGroupIds: on ? form.directGroupIds.filter((id) => id !== groupId) : [...form.directGroupIds, groupId],
  };
}
