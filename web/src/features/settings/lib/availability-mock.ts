import { normalizeVietnamese } from "@/lib/search";

/**
 * TEMPORARY MOCK DATA for the "Kho & Món Tạm Hết" cards.
 *
 * The design (design-system/pos-cafe/pages/settings.html, tab "Kho & Món Tạm
 * Hết") shows a thumbnail and a price on every card, but
 * `GET /catalog/menu/availability` carries neither: the catalog has no image
 * column at all, and the availability projection omits prices. Until the
 * backend provides them (docs/backlog/availability-card-fields.md), the cards
 * read these values from here so the screen can match the design.
 *
 * Prices are keyed by normalized name and mirror `scripts/dev-seed.ts`; an
 * unknown name has no mock price and the card shows "—". Replace every use of
 * this module with API fields once the backend work lands.
 */

/** Base price shown on the card: the smallest Size price, or the item price. */
const MOCK_PRICE_VND: Record<string, number> = {
  "croissant bo toi": 35_000,
  "ca phe den": 25_000,
  "ca phe sua da": 29_000,
  "bac xiu": 32_000,
  "tra dao cam sa": 45_000,
  "100% duong": 0,
  "70% duong": 0,
  "50% duong": 0,
  "khong duong": 0,
  "tran chau trang": 5_000,
  "thach nha dam": 5_000,
  "kem pho mai": 10_000,
};

export function mockPriceVnd(name: string): number | null {
  return MOCK_PRICE_VND[normalizeVietnamese(name)] ?? null;
}

/**
 * Mock thumbnail, the same placeholder service the design prototype uses. The
 * id is derived from the entity id so a card keeps its picture across renders.
 * A failed load falls back to the category icon, as in the design.
 */
export function mockImageUrl(entityId: string): string {
  let hash = 0;
  for (const ch of entityId) hash = (hash * 31 + ch.charCodeAt(0)) >>> 0;
  return `https://placewaifu.com/image/300/200?id=${(hash % 30) + 1}`;
}
