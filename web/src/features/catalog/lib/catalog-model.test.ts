// web/src/features/catalog/lib/catalog-model.test.ts
import { describe, expect, it } from "bun:test";
import type {
  CatalogManagementMenuResponse,
  CatalogManagementModifierGroupResponse,
} from "@/api/generated/models";
import { formatVND } from "@/lib/utils";
import {
  currentAssignment,
  displayCode,
  filterItems,
  priceLabel,
  ruleLabel,
  sameSet,
  toCatalogModel,
} from "./catalog-model";

const groups: CatalogManagementModifierGroupResponse[] = [
  {
    id: "g-ice",
    name: "Mức đá",
    min_selections: 1,
    max_selections: 1,
    default_option_ids: ["o-full", "o-gone"],
    options: [
      { id: "o-full", name: "100% đá", surcharge_vnd: 0, available: true },
      { id: "o-gone", name: "Đá riêng", surcharge_vnd: 0, available: true, retired: true },
    ],
  },
  { id: "g-old", name: "Nhóm cũ", min_selections: 0, max_selections: 1, retired: true, options: [] },
];

const menu: CatalogManagementMenuResponse = {
  categories: [
    {
      id: "c-tea",
      name: "Trà",
      display_order: 2,
      modifier_group_ids: ["g-ice", "g-old"],
      items: [
        {
          id: "i-peach",
          category_id: "c-tea",
          name: "Trà đào cam sả",
          code: "tdcs",
          badge: "HOT",
          price_vnd: 45000,
          direct_modifier_group_ids: ["g-old"],
          excluded_modifier_group_ids: [],
        },
        { id: "i-dead", category_id: "c-tea", name: "Trà cũ", retired: true, price_vnd: 1 },
      ],
    },
    {
      id: "c-coffee",
      name: "Cà phê",
      display_order: 1,
      modifier_group_ids: [],
      items: [
        {
          id: "i-milk",
          category_id: "c-coffee",
          name: "Cà phê sữa đá",
          badge: "WHATEVER",
          sizes: [
            { id: "s-m", name: "M", price_vnd: 29000, available: true },
            { id: "s-l", name: "L", price_vnd: 35000, available: false },
            { id: "s-x", name: "XL", price_vnd: 1, retired: true },
          ],
          direct_modifier_group_ids: ["g-ice"],
          excluded_modifier_group_ids: [],
        },
      ],
    },
    { id: "c-gone", name: "Đã bỏ", retired: true, display_order: 0, items: [] },
  ],
};

describe("toCatalogModel", () => {
  const model = toCatalogModel(menu, groups);

  it("drops retired entities and orders categories by display order", () => {
    expect(model.categories.map((c) => c.id)).toEqual(["c-coffee", "c-tea"]);
    expect(model.items.map((i) => i.id)).toEqual(["i-milk", "i-peach"]);
    expect(model.items[0].sizes.map((s) => s.id)).toEqual(["s-m", "s-l"]);
    expect(model.groups.map((g) => g.id)).toEqual(["g-ice"]);
    expect(model.groups[0].options.map((o) => o.id)).toEqual(["o-full"]);
    expect(model.groups[0].defaultOptionIds).toEqual(["o-full"]);
  });

  it("drops references to retired groups", () => {
    expect(model.categories[1].groupIds).toEqual(["g-ice"]);
    expect(model.items[1].directGroupIds).toEqual([]);
  });

  it("keeps only known badges and marks sized items with a null price", () => {
    expect(model.items[1].badge).toBe("HOT");
    expect(model.items[0].badge).toBeNull();
    expect(model.items[0].priceVnd).toBeNull();
    expect(model.items[1].priceVnd).toBe(45000);
  });

  it("handles missing responses", () => {
    expect(toCatalogModel(undefined, undefined)).toEqual({ categories: [], items: [], groups: [] });
  });
});

describe("display helpers", () => {
  const model = toCatalogModel(menu, groups);

  it("falls back to the name acronym when no code is stored", () => {
    expect(displayCode(model.items[0])).toBe("cfsd");
    expect(displayCode(model.items[1])).toBe("tdcs");
  });

  it("labels a single price, a range, and an empty size list", () => {
    expect(priceLabel(model.items[1])).toBe(formatVND(45000));
    expect(priceLabel(model.items[0])).toBe(`${formatVND(29000)} – ${formatVND(35000)}`);
    expect(priceLabel({ priceVnd: null, sizes: [] })).toBe("—");
  });

  it("describes selection rules", () => {
    expect(ruleLabel({ min: 1, max: 1 })).toBe("Chọn 1 · bắt buộc");
    expect(ruleLabel({ min: 0, max: 1 })).toBe("Chọn 1");
    expect(ruleLabel({ min: 1, max: 3 })).toBe("Chọn 1–3");
    expect(ruleLabel({ min: 0, max: 5 })).toBe("Tối đa 5");
  });

  it("filters by category, name without diacritics, and code", () => {
    expect(filterItems(model, "", "all").map((i) => i.id)).toEqual(["i-milk", "i-peach"]);
    expect(filterItems(model, "", "c-tea").map((i) => i.id)).toEqual(["i-peach"]);
    expect(filterItems(model, "sua da", "all").map((i) => i.id)).toEqual(["i-milk"]);
    expect(filterItems(model, "TDC", "all").map((i) => i.id)).toEqual(["i-peach"]);
  });

  it("reports a group's direct attachments", () => {
    expect(currentAssignment(model, "g-ice")).toEqual({ itemIds: ["i-milk"], categoryIds: ["c-tea"] });
  });

  it("compares id lists as sets", () => {
    expect(sameSet(["a", "b"], ["b", "a"])).toBe(true);
    expect(sameSet(["a"], ["a", "b"])).toBe(false);
  });
});
