import { getAcronym, matchesSearch, normalizeVietnamese } from "@/lib/search";
import {
  ALL_SCOPE,
  TOPPINGS_SCOPE,
  type AvailabilityFilter,
  type AvailabilitySizeView,
  type AvailabilityView,
} from "./availability";

/** Icon keys for card thumbnails and category pills (lucide names in the design). */
export type StockIcon = "coffee" | "cup-soda" | "sparkles" | "cake" | "layers" | "utensils";

/**
 * One card on the "Kho & Món Tạm Hết" grid: a Menu Item, or a Modifier Option
 * shown as a topping. The design lists both on the same grid.
 */
export interface StockCard {
  kind: "item" | "modifier_option";
  id: string;
  name: string;
  /** Short code shown next to the name ("CFSD"): the stored code, else derived from the name. */
  code: string;
  /** Category (item) or Modifier Group (topping) name. */
  groupName: string;
  /** Item: the Category id. Topping: TOPPINGS_SCOPE. */
  scope: string;
  /** Text in parentheses after the group name: "S / M / L", "1 phần thêm". */
  detail: string;
  available: boolean;
  /** Item Sizes, each with its own switch. Empty for toppings and unsized items. */
  sizes: AvailabilitySizeView[];
  /** Item only: why it cannot be sold although it is on (see blockedMessage). */
  blockedBy: string[];
  icon: StockIcon;
  /** Item price, lowest Size price, or topping surcharge; null when the caller cannot see prices. */
  priceVnd: number | null;
  /** Served from /media/catalog; null when the item has no image. */
  imageUrl: string | null;
}

export interface StockSummary {
  items: number;
  toppings: number;
  total: number;
  available: number;
  unavailable: number;
}

export function categoryIcon(categoryName: string): StockIcon {
  const n = normalizeVietnamese(categoryName);
  if (n.includes("ca phe") || n.includes("coffee")) return "coffee";
  if (n.includes("da xay") || n.includes("freeze") || n.includes("sinh to")) return "sparkles";
  if (n.includes("tra") || n.includes("tea")) return "cup-soda";
  if (n.includes("banh") || n.includes("bakery")) return "cake";
  return "utensils";
}

function sizeLabel(name: string): string {
  return name.replace(/^size\s+/i, "");
}

const SIZE_RANK: Record<string, number> = { xs: 0, s: 1, m: 2, l: 3, xl: 4, xxl: 5 };

/** Small to large when every Size is a letter size (S / M / L); otherwise the API order. */
export function orderSizes<T extends { name: string }>(sizes: T[]): T[] {
  const ranks = sizes.map((s) => SIZE_RANK[sizeLabel(s.name).toLowerCase()]);
  if (ranks.some((r) => r === undefined)) return sizes;
  return sizes.map((s, i) => ({ s, r: ranks[i] as number })).sort((a, b) => a.r - b.r).map((x) => x.s);
}

export function toStockCards(view: AvailabilityView): StockCard[] {
  const items: StockCard[] = view.items.map((item) => {
    const sizes = orderSizes(item.sizes);
    return {
      kind: "item",
      id: item.id,
      name: item.name,
      code: (item.code ?? getAcronym(item.name)).toUpperCase(),
      groupName: item.categoryName,
      scope: item.categoryId,
      detail: sizes.map((s) => sizeLabel(s.name)).join(" / "),
      available: item.available,
      sizes,
      blockedBy: item.blockedBy,
      icon: categoryIcon(item.categoryName),
      priceVnd: item.priceVnd,
      imageUrl: item.imageUrl,
    };
  });
  const toppings: StockCard[] = view.groups.flatMap((group) =>
    group.options.map((option) => ({
      kind: "modifier_option" as const,
      id: option.id,
      name: option.name,
      code: `TOP-${getAcronym(option.name).toUpperCase()}`,
      groupName: group.name,
      scope: TOPPINGS_SCOPE,
      detail: "1 phần thêm",
      available: option.available,
      sizes: [],
      blockedBy: [],
      icon: "layers" as const,
      priceVnd: option.priceVnd,
      imageUrl: null,
    })),
  );
  return [...items, ...toppings];
}

/** Counts cards the way the design does: one per Menu Item and per topping. */
export function summarizeCards(cards: StockCard[]): StockSummary {
  const items = cards.filter((c) => c.kind === "item").length;
  const unavailable = cards.filter((c) => !c.available).length;
  return { items, toppings: cards.length - items, total: cards.length, available: cards.length - unavailable, unavailable };
}

/** A card with anything switched off: the card itself or one of its Sizes. */
export function hasUnavailable(card: StockCard): boolean {
  return !card.available || card.sizes.some((s) => !s.available);
}

export function filterCards(cards: StockCard[], filter: AvailabilityFilter): StockCard[] {
  const query = filter.query.trim();
  return cards.filter((card) => {
    if (filter.scope === TOPPINGS_SCOPE && card.kind !== "modifier_option") return false;
    if (filter.scope !== ALL_SCOPE && filter.scope !== TOPPINGS_SCOPE && card.scope !== filter.scope) return false;
    if (filter.onlyUnavailable && !hasUnavailable(card)) return false;
    if (!query) return true;
    return (
      matchesSearch(query, card.name, card.groupName) ||
      normalizeVietnamese(card.code).includes(normalizeVietnamese(query)) ||
      card.sizes.some((s) => matchesSearch(query, s.name))
    );
  });
}
