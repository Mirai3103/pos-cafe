// web/src/features/catalog/lib/catalog-model.ts
import type {
  CatalogManagementItemResponse,
  CatalogManagementMenuResponse,
  CatalogManagementModifierGroupResponse,
} from "@/api/generated/models";
import { getAcronym, normalizeVietnamese } from "@/lib/search";
import { formatVND } from "@/lib/utils";

export type Badge = "BEST_SELLER" | "HOT" | "NEW" | "SIGNATURE" | "CHEF_PICK";

/** Labels from settings.html's badge selector. */
export const BADGE_LABELS: Record<Badge, string> = {
  BEST_SELLER: "Bán chạy",
  HOT: "Món hot",
  NEW: "Mới",
  SIGNATURE: "Đặc trưng",
  CHEF_PICK: "Chef Pick",
};

export const BADGES = Object.keys(BADGE_LABELS) as Badge[];

export type RetireReason = "NO_LONGER_OFFERED" | "MENU_RESTRUCTURE" | "OTHER";

export const RETIRE_REASON_LABELS: Record<RetireReason, string> = {
  NO_LONGER_OFFERED: "Không bán nữa",
  MENU_RESTRUCTURE: "Sắp xếp lại thực đơn",
  OTHER: "Khác",
};

export const RETIRE_REASONS = Object.keys(RETIRE_REASON_LABELS) as RetireReason[];

export interface Retirement {
  reason: RetireReason;
  note: string;
}

export interface CatSize {
  id: string;
  name: string;
  priceVnd: number;
  available: boolean;
}

export interface CatItem {
  id: string;
  categoryId: string;
  name: string;
  code: string | null;
  badge: Badge | null;
  description: string | null;
  imageUrl: string | null;
  /** Set for a single-price item; null when the item is priced by Sizes (ADR-060). */
  priceVnd: number | null;
  sizes: CatSize[];
  directGroupIds: string[];
  excludedGroupIds: string[];
}

export interface CatCategory {
  id: string;
  name: string;
  icon: string | null;
  displayOrder: number;
  groupIds: string[];
}

export interface CatOption {
  id: string;
  name: string;
  surchargeVnd: number;
  available: boolean;
}

export interface CatGroup {
  id: string;
  name: string;
  min: number;
  max: number;
  options: CatOption[];
  defaultOptionIds: string[];
}

export interface CatalogModel {
  categories: CatCategory[];
  items: CatItem[];
  groups: CatGroup[];
}

/** A modifier group's direct attachments: what the Batch Linker edits. */
export interface Assignment {
  itemIds: string[];
  categoryIds: string[];
}

export const EMPTY_CATALOG: CatalogModel = { categories: [], items: [], groups: [] };

function toBadge(value: string | undefined): Badge | null {
  return value !== undefined && value in BADGE_LABELS ? (value as Badge) : null;
}

function toGroup(g: CatalogManagementModifierGroupResponse): CatGroup {
  const options = (g.options ?? [])
    .filter((o) => !o.retired)
    .map((o) => ({
      id: o.id ?? "",
      name: o.name ?? "",
      surchargeVnd: o.surcharge_vnd ?? 0,
      available: o.available ?? false,
    }));
  const live = new Set(options.map((o) => o.id));
  return {
    id: g.id ?? "",
    name: g.name ?? "",
    min: g.min_selections ?? 0,
    max: g.max_selections ?? 1,
    options,
    defaultOptionIds: (g.default_option_ids ?? []).filter((id) => live.has(id)),
  };
}

function toItem(i: CatalogManagementItemResponse, keep: (ids?: string[]) => string[]): CatItem {
  return {
    id: i.id ?? "",
    categoryId: i.category_id ?? "",
    name: i.name ?? "",
    code: i.code ?? null,
    badge: toBadge(i.badge),
    description: i.description ?? null,
    imageUrl: i.image_url ?? null,
    priceVnd: i.price_vnd ?? null,
    sizes: (i.sizes ?? [])
      .filter((s) => !s.retired)
      .map((s) => ({ id: s.id ?? "", name: s.name ?? "", priceVnd: s.price_vnd ?? 0, available: s.available ?? false })),
    directGroupIds: keep(i.direct_modifier_group_ids),
    excludedGroupIds: keep(i.excluded_modifier_group_ids),
  };
}

/**
 * Flattens the management projection into the non-retired catalog the admin
 * screens edit. Retired entities have no reinstate command, so they are
 * dropped, and so are references to retired groups.
 */
export function toCatalogModel(
  menu: CatalogManagementMenuResponse | undefined,
  groups: CatalogManagementModifierGroupResponse[] | undefined,
): CatalogModel {
  const liveGroups = (groups ?? []).filter((g) => !g.retired).map(toGroup);
  const liveIds = new Set(liveGroups.map((g) => g.id));
  const keep = (ids?: string[]) => (ids ?? []).filter((id) => liveIds.has(id));

  const live = (menu?.categories ?? [])
    .filter((c) => !c.retired)
    .map((raw) => ({
      raw,
      category: {
        id: raw.id ?? "",
        name: raw.name ?? "",
        icon: raw.icon ?? null,
        displayOrder: raw.display_order ?? 0,
        groupIds: keep(raw.modifier_group_ids),
      },
    }));
  live.sort(
    (a, b) =>
      a.category.displayOrder - b.category.displayOrder || a.category.name.localeCompare(b.category.name, "vi"),
  );

  return {
    categories: live.map((l) => l.category),
    items: live.flatMap((l) => (l.raw.items ?? []).filter((i) => !i.retired).map((i) => toItem(i, keep))),
    groups: liveGroups,
  };
}

export function displayCode(item: Pick<CatItem, "code" | "name">): string {
  return item.code ?? getAcronym(item.name);
}

export function priceLabel(item: Pick<CatItem, "priceVnd" | "sizes">): string {
  if (item.priceVnd !== null) return formatVND(item.priceVnd);
  const prices = item.sizes.map((s) => s.priceVnd);
  if (prices.length === 0) return "—";
  const lo = Math.min(...prices);
  const hi = Math.max(...prices);
  return lo === hi ? formatVND(lo) : `${formatVND(lo)} – ${formatVND(hi)}`;
}

export function ruleLabel(group: Pick<CatGroup, "min" | "max">): string {
  if (group.max === 1) return group.min >= 1 ? "Chọn 1 · bắt buộc" : "Chọn 1";
  return group.min > 0 ? `Chọn ${group.min}–${group.max}` : `Tối đa ${group.max}`;
}

/** Name matches ignore diacritics; code matches are prefix matches on the shown code. */
export function filterItems(model: CatalogModel, query: string, categoryId: string): CatItem[] {
  const q = normalizeVietnamese(query);
  const compact = q.replace(/\s/g, "");
  return model.items.filter((item) => {
    if (categoryId !== "all" && item.categoryId !== categoryId) return false;
    if (!q) return true;
    return normalizeVietnamese(item.name).includes(q) || displayCode(item).toLowerCase().startsWith(compact);
  });
}

export function currentAssignment(model: CatalogModel, groupId: string): Assignment {
  return {
    itemIds: model.items.filter((i) => i.directGroupIds.includes(groupId)).map((i) => i.id),
    categoryIds: model.categories.filter((c) => c.groupIds.includes(groupId)).map((c) => c.id),
  };
}

/** Id lists never hold duplicates, so equal length plus containment is set equality. */
export function sameSet(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((id) => b.includes(id));
}
