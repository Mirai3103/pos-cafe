// web/src/features/catalog/lib/forms.test.ts
import { describe, expect, it } from "bun:test";
import type { CatCategory, CatGroup, CatItem } from "./catalog-model";
import {
  categoryFormFrom,
  groupFormFrom,
  itemFormFrom,
  removeOptionRow,
  removeSizeRow,
  selectionType,
  setSelectionType,
  toggleDefault,
} from "./forms";

const latte: CatItem = {
  id: "i-latte",
  categoryId: "c-coffee",
  name: "Latte",
  code: "lt",
  badge: "HOT",
  description: null,
  imageUrl: null,
  priceVnd: null,
  sizes: [
    { id: "s-m", name: "M", priceVnd: 39000, available: true },
    { id: "s-l", name: "L", priceVnd: 45000, available: true },
  ],
  directGroupIds: ["g-top"],
  excludedGroupIds: [],
};

const topping: CatGroup = {
  id: "g-top",
  name: "Topping",
  min: 0,
  max: 2,
  options: [
    { id: "o-pearl", name: "Trân châu", surchargeVnd: 10000, available: true },
    { id: "o-jelly", name: "Thạch", surchargeVnd: 8000, available: true },
  ],
  defaultOptionIds: ["o-jelly"],
};

const retire = { reason: "NO_LONGER_OFFERED" as const, note: "" };

describe("item form", () => {
  it("starts a new item as single price in the given category", () => {
    const form = itemFormFrom(null, "c-tea");
    expect(form.mode).toBe("single");
    expect(form.categoryId).toBe("c-tea");
    expect(form.sizes).toEqual([]);
  });

  it("loads a sized item with rows keyed by size id", () => {
    const form = itemFormFrom(latte, "c-tea");
    expect(form.mode).toBe("sizes");
    expect(form.sizes.map((s) => [s.key, s.id, s.name])).toEqual([
      ["s-m", "s-m", "M"],
      ["s-l", "s-l", "L"],
    ]);
    expect(form.code).toBe("lt");
    expect(form.directGroupIds).toEqual(["g-top"]);
  });

  it("queues a saved size for retirement and drops an unsaved one", () => {
    const form = itemFormFrom(latte, "c-tea");
    const withNew = { ...form, sizes: [...form.sizes, { key: "k-new", id: null, name: "XL", priceVnd: 1 }] };
    const retired = removeSizeRow(withNew, "s-l", retire);
    expect(retired.sizes.map((s) => s.key)).toEqual(["s-m", "k-new"]);
    expect(retired.retiredSizes).toEqual([{ id: "s-l", name: "L", retirement: retire }]);
    const dropped = removeSizeRow(withNew, "k-new", null);
    expect(dropped.retiredSizes).toEqual([]);
    expect(dropped.sizes.map((s) => s.key)).toEqual(["s-m", "s-l"]);
  });
});

describe("category form", () => {
  it("suggests the next display order for a new category", () => {
    const cats: CatCategory[] = [
      { id: "a", name: "A", icon: null, displayOrder: 3, groupIds: [] },
      { id: "b", name: "B", icon: null, displayOrder: 7, groupIds: [] },
    ];
    expect(categoryFormFrom(null, cats).displayOrder).toBe(8);
    expect(categoryFormFrom(cats[0], cats)).toEqual({ name: "A", icon: null, displayOrder: 3, groupIds: [] });
  });
});

describe("group form", () => {
  it("marks stored defaults on their rows", () => {
    const form = groupFormFrom(topping);
    expect(form.rows.map((r) => [r.id, r.isDefault])).toEqual([
      ["o-pearl", false],
      ["o-jelly", true],
    ]);
  });

  it("starts a new group with one blank row", () => {
    const form = groupFormFrom(null);
    expect(form.rows).toHaveLength(1);
    expect(form.min).toBe(0);
    expect(form.max).toBe(1);
  });

  it("keeps at most one default in a single-choice group", () => {
    const single = setSelectionType(groupFormFrom(topping), "single");
    expect(single.max).toBe(1);
    const toggled = toggleDefault(single, "o-pearl");
    expect(toggled.rows.map((r) => r.isDefault)).toEqual([true, false]);
  });

  it("lets a multiple-choice group hold several defaults", () => {
    const toggled = toggleDefault(groupFormFrom(topping), "o-pearl");
    expect(toggled.rows.map((r) => r.isDefault)).toEqual([true, true]);
  });

  it("derives the selection type from max and widens it for multiple", () => {
    expect(selectionType({ max: 1 })).toBe("single");
    expect(selectionType({ max: 3 })).toBe("multiple");
    expect(setSelectionType(groupFormFrom(null), "multiple").max).toBe(2);
  });

  it("queues a saved option for retirement", () => {
    const form = removeOptionRow(groupFormFrom(topping), "o-pearl", retire);
    expect(form.rows.map((r) => r.id)).toEqual(["o-jelly"]);
    expect(form.retiredOptions).toEqual([{ id: "o-pearl", name: "Trân châu", retirement: retire }]);
  });
});
