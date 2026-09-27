// web/src/features/catalog/lib/linker.test.ts
import { describe, expect, it } from "bun:test";
import type { CatalogModel, CatItem } from "./catalog-model";
import { clearCategoryItems, linkState, sameAssignment, selectAllInCategory, toggleCategory, toggleItem } from "./linker";

function item(id: string, categoryId: string, excluded: string[] = []): CatItem {
  return {
    id,
    categoryId,
    name: id,
    code: null,
    badge: null,
    description: null,
    imageUrl: null,
    priceVnd: 1000,
    sizes: [],
    directGroupIds: [],
    excludedGroupIds: excluded,
  };
}

const model: CatalogModel = {
  categories: [
    { id: "c-tea", name: "Trà", icon: null, displayOrder: 1, groupIds: ["g-ice"] },
    { id: "c-cake", name: "Bánh", icon: null, displayOrder: 2, groupIds: [] },
  ],
  items: [item("t1", "c-tea"), item("t2", "c-tea", ["g-ice"]), item("k1", "c-cake")],
  groups: [],
};

describe("linker", () => {
  it("classifies each item for the selected group", () => {
    const draft = { itemIds: ["k1"], categoryIds: ["c-tea"] };
    expect(linkState(model.items[0], "g-ice", draft)).toBe("inherited");
    expect(linkState(model.items[1], "g-ice", draft)).toBe("excluded");
    expect(linkState(model.items[2], "g-ice", draft)).toBe("direct");
    expect(linkState(model.items[2], "g-ice", { itemIds: [], categoryIds: [] })).toBe("none");
  });

  it("toggles items and categories", () => {
    const empty = { itemIds: [], categoryIds: [] };
    expect(toggleItem(empty, "k1")).toEqual({ itemIds: ["k1"], categoryIds: [] });
    expect(toggleItem({ itemIds: ["k1"], categoryIds: [] }, "k1")).toEqual(empty);
    expect(toggleCategory(empty, "c-tea")).toEqual({ itemIds: [], categoryIds: ["c-tea"] });
  });

  it("selects every item of a category except those excluding the group", () => {
    const next = selectAllInCategory({ itemIds: ["k1"], categoryIds: [] }, model, "g-ice", "c-tea");
    expect(next.itemIds.sort()).toEqual(["k1", "t1"]);
    expect(clearCategoryItems(next, model, "c-tea").itemIds).toEqual(["k1"]);
  });

  it("compares drafts as sets", () => {
    expect(sameAssignment({ itemIds: ["a", "b"], categoryIds: [] }, { itemIds: ["b", "a"], categoryIds: [] })).toBe(true);
    expect(sameAssignment({ itemIds: [], categoryIds: ["c"] }, { itemIds: [], categoryIds: [] })).toBe(false);
  });
});
