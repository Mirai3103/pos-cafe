import type {
  CatalogAvailabilityChange,
  CatalogAvailabilityMenuResponse,
} from "@/api/generated/models";
import { newRequestId } from "@/lib/command";
import { matchesSearch } from "@/lib/search";

export type AvailabilityKind = "item" | "size" | "modifier_option";

export interface AvailabilityRef {
  kind: AvailabilityKind;
  id: string;
  name: string;
}

export interface AvailabilitySizeView {
  id: string;
  name: string;
  available: boolean;
}

export interface AvailabilityItemView {
  id: string;
  name: string;
  categoryId: string;
  categoryName: string;
  available: boolean;
  sizes: AvailabilitySizeView[];
  /** Why an item that is itself on still cannot be sold (mirrors catalog.IsSellable). */
  blockedBy: string[];
}

export interface AvailabilityOptionView {
  id: string;
  name: string;
  groupId: string;
  groupName: string;
  available: boolean;
}

export interface AvailabilityGroupView {
  id: string;
  name: string;
  options: AvailabilityOptionView[];
}

export interface AvailabilityStats {
  total: number;
  available: number;
  unavailable: number;
}

export interface AvailabilityView {
  categories: { id: string; name: string }[];
  items: AvailabilityItemView[];
  groups: AvailabilityGroupView[];
  stats: AvailabilityStats;
  unavailableRefs: AvailabilityRef[];
}

export interface AvailabilityFilter {
  query: string;
  /** ALL_SCOPE, TOPPINGS_SCOPE, or a category id. */
  scope: string;
  onlyUnavailable: boolean;
}

export const SIZE_BLOCK_LABEL = "Kích cỡ";
export const ALL_SCOPE = "all";
export const TOPPINGS_SCOPE = "toppings";
export const EMPTY_FILTER: AvailabilityFilter = { query: "", scope: ALL_SCOPE, onlyUnavailable: false };

export function toAvailabilityView(menu: CatalogAvailabilityMenuResponse | null | undefined): AvailabilityView {
  const categories: { id: string; name: string }[] = [];
  const items: AvailabilityItemView[] = [];
  const groups = new Map<string, AvailabilityGroupView>();
  const seenOptions = new Set<string>();

  for (const cat of menu?.categories ?? []) {
    const categoryId = cat.id ?? "";
    const categoryName = cat.name ?? "";
    categories.push({ id: categoryId, name: categoryName });

    for (const item of cat.items ?? []) {
      const sizes = (item.sizes ?? []).map((s) => ({
        id: s.id ?? "",
        name: s.name ?? "",
        available: s.available ?? false,
      }));
      const blockedBy: string[] = [];
      if (sizes.length > 0 && !sizes.some((s) => s.available)) blockedBy.push(SIZE_BLOCK_LABEL);

      for (const g of item.modifier_groups ?? []) {
        const options = g.options ?? [];
        const availableCount = options.filter((o) => o.available).length;
        if ((g.min_selections ?? 0) > availableCount) blockedBy.push(g.name ?? "");

        const groupId = g.id ?? "";
        let group = groups.get(groupId);
        if (!group) {
          group = { id: groupId, name: g.name ?? "", options: [] };
          groups.set(groupId, group);
        }
        for (const o of options) {
          const optionId = o.id ?? "";
          if (seenOptions.has(optionId)) continue;
          seenOptions.add(optionId);
          group.options.push({
            id: optionId,
            name: o.name ?? "",
            groupId,
            groupName: group.name,
            available: o.available ?? false,
          });
        }
      }

      items.push({
        id: item.id ?? "",
        name: item.name ?? "",
        categoryId,
        categoryName,
        available: item.available ?? false,
        sizes,
        blockedBy,
      });
    }
  }

  const groupList = [...groups.values()].filter((g) => g.options.length > 0);
  const unavailableRefs: AvailabilityRef[] = [];
  let total = 0;
  for (const item of items) {
    for (const size of item.sizes) {
      total += 1;
      if (!size.available) unavailableRefs.push({ kind: "size", id: size.id, name: `${item.name} (${size.name})` });
    }
    total += 1;
    if (!item.available) unavailableRefs.push({ kind: "item", id: item.id, name: item.name });
  }
  for (const group of groupList) {
    for (const option of group.options) {
      total += 1;
      if (!option.available) {
        unavailableRefs.push({ kind: "modifier_option", id: option.id, name: `${option.name} (${group.name})` });
      }
    }
  }

  return {
    categories,
    items,
    groups: groupList,
    stats: { total, available: total - unavailableRefs.length, unavailable: unavailableRefs.length },
    unavailableRefs,
  };
}

export function blockedMessage(blockedBy: string[]): string | null {
  if (blockedBy.length === 0) return null;
  const parts: string[] = [];
  if (blockedBy.includes(SIZE_BLOCK_LABEL)) parts.push("hết kích cỡ");
  const groupNames = blockedBy.filter((b) => b !== SIZE_BLOCK_LABEL);
  if (groupNames.length > 0) parts.push(`hết tùy chọn bắt buộc (${groupNames.join(", ")})`);
  return `Không bán được: ${parts.join("; ")}`;
}

export function isItemFullyAvailable(item: AvailabilityItemView): boolean {
  return item.available && item.sizes.every((s) => s.available);
}

export function filterItems(items: AvailabilityItemView[], filter: AvailabilityFilter): AvailabilityItemView[] {
  if (filter.scope === TOPPINGS_SCOPE) return [];
  return items.filter((item) => {
    if (filter.scope !== ALL_SCOPE && item.categoryId !== filter.scope) return false;
    if (filter.onlyUnavailable && isItemFullyAvailable(item)) return false;
    if (!filter.query.trim()) return true;
    return (
      matchesSearch(filter.query, item.name, item.categoryName) ||
      item.sizes.some((s) => matchesSearch(filter.query, s.name))
    );
  });
}

export function filterGroups(groups: AvailabilityGroupView[], filter: AvailabilityFilter): AvailabilityGroupView[] {
  if (filter.scope !== ALL_SCOPE && filter.scope !== TOPPINGS_SCOPE) return [];
  return groups
    .map((g) => ({
      ...g,
      options: g.options.filter((o) => {
        if (filter.onlyUnavailable && o.available) return false;
        return matchesSearch(filter.query, o.name, g.name);
      }),
    }))
    .filter((g) => g.options.length > 0);
}

/** Optimistic patch: sets one entity's availability everywhere it appears. */
export function applyAvailability(
  menu: CatalogAvailabilityMenuResponse,
  kind: AvailabilityKind,
  id: string,
  available: boolean,
): CatalogAvailabilityMenuResponse {
  return {
    ...menu,
    categories: menu.categories?.map((cat) => ({
      ...cat,
      items: cat.items?.map((item) => ({
        ...item,
        available: kind === "item" && item.id === id ? available : item.available,
        sizes: item.sizes?.map((s) => (kind === "size" && s.id === id ? { ...s, available } : s)),
        modifier_groups: item.modifier_groups?.map((g) => ({
          ...g,
          options: g.options?.map((o) => (kind === "modifier_option" && o.id === id ? { ...o, available } : o)),
        })),
      })),
    })),
  };
}

export function toRestoreChanges(refs: AvailabilityRef[]): CatalogAvailabilityChange[] {
  return refs.map((r) => ({ kind: r.kind, id: r.id, available: true }));
}

export function refsKey(refs: AvailabilityRef[]): string {
  return refs.map((r) => `${r.kind}:${r.id}`).join(",");
}

export interface Intent {
  key: string;
  id: string;
}

/**
 * One request_id per restore intent: kept while the list the operator sees is
 * unchanged, renewed when it changes, because a different list is a
 * different command and would otherwise be refused as REQUEST_CONFLICT.
 */
export function nextIntent(prev: Intent | null, key: string): Intent {
  if (prev && prev.key === key) return prev;
  return { key, id: newRequestId() };
}
