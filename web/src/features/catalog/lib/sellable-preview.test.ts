// web/src/features/catalog/lib/sellable-preview.test.ts
import { describe, expect, it } from "bun:test";
import type { CatalogModel, CatItem } from "./catalog-model";
import { toSellablePreview } from "./sellable-preview";

const tea: CatItem = {
  id: "i-tea",
  categoryId: "c-tea",
  name: "Trà đào",
  code: null,
  badge: null,
  description: null,
  imageUrl: null,
  priceVnd: 45000,
  sizes: [],
  directGroupIds: ["g-top"],
  excludedGroupIds: [],
};

const latte: CatItem = {
  ...tea,
  id: "i-latte",
  name: "Latte",
  priceVnd: null,
  sizes: [
    { id: "s-m", name: "M", priceVnd: 39000, available: true },
    { id: "s-l", name: "L", priceVnd: 45000, available: false },
  ],
  directGroupIds: [],
  excludedGroupIds: ["g-ice"],
};

const model: CatalogModel = {
  categories: [{ id: "c-tea", name: "Trà", icon: null, displayOrder: 1, groupIds: ["g-ice"] }],
  items: [tea, latte],
  groups: [
    {
      id: "g-ice",
      name: "Mức đá",
      min: 1,
      max: 1,
      options: [
        { id: "o-full", name: "100% đá", surchargeVnd: 0, available: true },
        { id: "o-less", name: "Ít đá", surchargeVnd: 0, available: false },
      ],
      defaultOptionIds: ["o-full"],
    },
    {
      id: "g-top",
      name: "Topping",
      min: 0,
      max: 2,
      options: [{ id: "o-pearl", name: "Trân châu", surchargeVnd: 10000, available: true }],
      defaultOptionIds: [],
    },
  ],
};

describe("toSellablePreview", () => {
  it("offers inherited then direct groups with only available options", () => {
    const preview = toSellablePreview(tea, model);
    expect(preview.modifier_groups?.map((g) => g.id)).toEqual(["g-ice", "g-top"]);
    expect(preview.modifier_groups?.[0].options?.map((o) => o.id)).toEqual(["o-full"]);
    expect(preview.price_vnd).toBe(45000);
    expect(preview.sizes).toEqual([]);
  });

  it("drops excluded groups and unavailable sizes", () => {
    const preview = toSellablePreview(latte, model);
    expect(preview.modifier_groups).toEqual([]);
    expect(preview.sizes).toEqual([{ id: "s-m", name: "M", price_vnd: 39000 }]);
    expect(preview.price_vnd).toBeUndefined();
  });

  it("applies the linker's pending assignment for its group", () => {
    const off = toSellablePreview(tea, model, { groupId: "g-ice", itemIds: [], categoryIds: [] });
    expect(off.modifier_groups?.map((g) => g.id)).toEqual(["g-top"]);
    const direct = toSellablePreview(latte, model, { groupId: "g-top", itemIds: ["i-latte"], categoryIds: [] });
    expect(direct.modifier_groups?.map((g) => g.id)).toEqual(["g-top"]);
  });
});
