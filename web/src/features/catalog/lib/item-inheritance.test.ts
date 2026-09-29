// web/src/features/catalog/lib/item-inheritance.test.ts
import { describe, expect, it } from "bun:test";
import type { CatCategory } from "./catalog-model";
import { itemFormFrom } from "./forms";
import { changeCategory, effectiveGroupIds, inheritedGroupIds, toggleDirect, toggleExcluded } from "./item-inheritance";

const categories: CatCategory[] = [
  { id: "c-tea", name: "Trà", icon: null, displayOrder: 1, groupIds: ["g-ice", "g-sugar"] },
  { id: "c-cake", name: "Bánh", icon: null, displayOrder: 2, groupIds: ["g-sugar"] },
];

describe("item inheritance", () => {
  it("reads the category's groups", () => {
    expect(inheritedGroupIds(categories, "c-tea")).toEqual(["g-ice", "g-sugar"]);
    expect(inheritedGroupIds(categories, "missing")).toEqual([]);
  });

  it("offers inherited minus excluded, then direct groups", () => {
    expect(effectiveGroupIds(["g-ice", "g-sugar"], ["g-top", "g-sugar"], ["g-ice"])).toEqual(["g-sugar", "g-top"]);
  });

  it("drops exclusions the new category does not provide (ADR-059)", () => {
    const form = { ...itemFormFrom(null, "c-tea"), excludedGroupIds: ["g-ice", "g-sugar"] };
    expect(changeCategory(form, categories, "c-cake").excludedGroupIds).toEqual(["g-sugar"]);
  });

  it("removes a group from direct when it becomes excluded", () => {
    const form = { ...itemFormFrom(null, "c-tea"), directGroupIds: ["g-ice"] };
    const next = toggleExcluded(form, "g-ice");
    expect(next.excludedGroupIds).toEqual(["g-ice"]);
    expect(next.directGroupIds).toEqual([]);
    expect(toggleExcluded(next, "g-ice").excludedGroupIds).toEqual([]);
  });

  it("toggles a direct group", () => {
    const form = itemFormFrom(null, "c-tea");
    expect(toggleDirect(form, "g-top").directGroupIds).toEqual(["g-top"]);
    expect(toggleDirect(toggleDirect(form, "g-top"), "g-top").directGroupIds).toEqual([]);
  });
});
