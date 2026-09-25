import { describe, expect, it } from "bun:test";
import type { CatalogAvailabilityMenuResponse } from "@/api/generated/models";
import { ALL_SCOPE, EMPTY_FILTER, TOPPINGS_SCOPE, toAvailabilityView } from "./availability";
import { categoryIcon, filterCards, orderSizes, summarizeCards, toStockCards } from "./availability-cards";

const sugar = {
  id: "g-sugar",
  name: "Mức đường",
  min_selections: 1,
  max_selections: 1,
  options: [
    { id: "o-100", name: "100% đường", available: true },
    { id: "o-50", name: "50% đường", available: true },
  ],
};

const topping = {
  id: "g-top",
  name: "Topping thêm",
  min_selections: 0,
  max_selections: 3,
  options: [{ id: "o-pearl", name: "Trân châu trắng", available: false }],
};

const menu: CatalogAvailabilityMenuResponse = {
  categories: [
    {
      id: "c-coffee",
      name: "Cà phê",
      items: [
        {
          id: "i-den",
          name: "Cà phê đen",
          category_id: "c-coffee",
          available: true,
          sizes: [
            { id: "s-den-l", name: "Size L", available: false },
            { id: "s-den-s", name: "Size S", available: true },
          ],
          modifier_groups: [sugar, topping],
        },
        { id: "i-sua", name: "Cà phê sữa đá", category_id: "c-coffee", available: false, modifier_groups: [sugar, topping] },
      ],
    },
    {
      id: "c-cake",
      name: "Bánh ngọt",
      items: [{ id: "i-croissant", name: "Croissant bơ tỏi", category_id: "c-cake", available: true }],
    },
  ],
};

const cards = toStockCards(toAvailabilityView(menu));
const ids = (list: { id: string }[]) => list.map((c) => c.id);

describe("toStockCards", () => {
  it("lists items, then each topping once, on one grid", () => {
    expect(ids(cards)).toEqual(["i-den", "i-sua", "i-croissant", "o-100", "o-50", "o-pearl"]);
    expect(cards.map((c) => c.kind)).toEqual(["item", "item", "item", "modifier_option", "modifier_option", "modifier_option"]);
  });

  it("derives the code, the icon, and the size detail small to large", () => {
    const den = cards[0];
    expect(den.code).toBe("CFD");
    expect(den.icon).toBe("coffee");
    expect(den.detail).toBe("S / L");
    expect(ids(den.sizes)).toEqual(["s-den-s", "s-den-l"]);
    const pearl = cards[5];
    expect(pearl.code).toBe("TOP-TCT");
    expect(pearl.icon).toBe("layers");
    expect(pearl.groupName).toBe("Topping thêm");
    expect(pearl.detail).toBe("1 phần thêm");
  });

  it("fills mock price and image until the backend provides them", () => {
    expect(cards[2].priceVnd).toBe(35_000);
    expect(cards[2].imageUrl).toMatch(/^https:\/\//);
    expect(toStockCards(toAvailabilityView({ categories: [{ id: "c", name: "C", items: [{ id: "x", name: "Món lạ", available: true }] }] }))[0].priceVnd).toBeNull();
  });
});

describe("summarizeCards", () => {
  it("counts one per item and per topping, as the design does", () => {
    expect(summarizeCards(cards)).toEqual({ items: 3, toppings: 3, total: 6, available: 4, unavailable: 2 });
  });
});

describe("filterCards", () => {
  it("returns everything with the empty filter", () => {
    expect(filterCards(cards, EMPTY_FILTER)).toHaveLength(6);
  });

  it("searches without diacritics across names, codes, groups, and sizes", () => {
    expect(ids(filterCards(cards, { ...EMPTY_FILTER, query: "ca phe sua" }))).toEqual(["i-sua"]);
    expect(ids(filterCards(cards, { ...EMPTY_FILTER, query: "banh" }))).toEqual(["i-croissant"]);
    expect(ids(filterCards(cards, { ...EMPTY_FILTER, query: "cfsd" }))).toEqual(["i-sua"]);
    expect(ids(filterCards(cards, { ...EMPTY_FILTER, query: "top-tct" }))).toEqual(["o-pearl"]);
  });

  it("scopes to one category, hiding toppings", () => {
    expect(ids(filterCards(cards, { ...EMPTY_FILTER, scope: "c-cake" }))).toEqual(["i-croissant"]);
  });

  it("scopes to toppings, hiding items", () => {
    expect(ids(filterCards(cards, { ...EMPTY_FILTER, scope: TOPPINGS_SCOPE }))).toEqual(["o-100", "o-50", "o-pearl"]);
  });

  it("keeps only cards with something off, including a Size", () => {
    expect(ids(filterCards(cards, { ...EMPTY_FILTER, scope: ALL_SCOPE, onlyUnavailable: true }))).toEqual(["i-den", "i-sua", "o-pearl"]);
  });
});

describe("helpers", () => {
  it("maps category names to the design's icons", () => {
    expect(categoryIcon("Cà phê Việt")).toBe("coffee");
    expect(categoryIcon("Trà trái cây")).toBe("cup-soda");
    expect(categoryIcon("Đá xay")).toBe("sparkles");
    expect(categoryIcon("Bánh ngọt")).toBe("cake");
    expect(categoryIcon("Khác")).toBe("utensils");
  });

  it("orders letter sizes small to large and leaves other names alone", () => {
    expect(orderSizes([{ name: "Size L" }, { name: "M" }, { name: "Size S" }]).map((s) => s.name)).toEqual(["Size S", "M", "Size L"]);
    expect(orderSizes([{ name: "Ly lớn" }, { name: "Ly nhỏ" }]).map((s) => s.name)).toEqual(["Ly lớn", "Ly nhỏ"]);
  });
});
