// web/src/features/catalog/lib/save-plan.test.ts
import { describe, expect, it } from "bun:test";
import type { CatCategory, CatGroup, CatItem } from "./catalog-model";
import { categoryFormFrom, groupFormFrom, itemFormFrom, removeOptionRow, removeSizeRow } from "./forms";
import {
  NEW_ID,
  NEW_OPTION_PREFIX,
  planAssignments,
  planCategorySave,
  planGroupSave,
  planItemSave,
  planNeedsPin,
  planRetire,
  type Step,
} from "./save-plan";

const types = (steps: Step[]) => steps.map((s) => s.cmd.type);
const blob = new Blob(["x"], { type: "image/webp" });
const retire = { reason: "NO_LONGER_OFFERED" as const, note: "" };

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
  directGroupIds: [],
  excludedGroupIds: ["g-ice"],
};

const latte: CatItem = {
  id: "i-latte",
  categoryId: "c-coffee",
  name: "Latte",
  code: "lt",
  badge: "HOT",
  description: "Sữa tươi",
  imageUrl: "/media/catalog/a.webp",
  priceVnd: null,
  sizes: [
    { id: "s-m", name: "M", priceVnd: 39000, available: true },
    { id: "s-l", name: "L", priceVnd: 45000, available: true },
  ],
  directGroupIds: ["g-top"],
  excludedGroupIds: [],
};

describe("planItemSave", () => {
  it("plans nothing for an unchanged item", () => {
    expect(planItemSave(tea, itemFormFrom(tea, ""))).toEqual([]);
    expect(planItemSave(latte, itemFormFrom(latte, ""))).toEqual([]);
  });

  it("creates first and targets follow-up steps at the new id", () => {
    const form = {
      ...itemFormFrom(null, "c-tea"),
      name: " Trà vải ",
      priceVnd: 42000,
      code: "tv",
      image: { kind: "upload" as const, blob, previewUrl: "blob:x" },
      directGroupIds: ["g-top"],
    };
    const steps = planItemSave(null, form);
    expect(types(steps)).toEqual(["item.create", "item.details", "item.image.set", "item.groups"]);
    expect(steps[0].cmd).toEqual({ type: "item.create", categoryId: "c-tea", name: "Trà vải", priceVnd: 42000 });
    expect(steps.slice(1).every((s) => "itemId" in s.cmd && s.cmd.itemId === NEW_ID)).toBe(true);
  });

  it("creates a sized item with its sizes and nothing else when details are empty", () => {
    const form = {
      ...itemFormFrom(null, "c-tea"),
      name: "Trà sữa",
      mode: "sizes" as const,
      sizes: [
        { key: "a", id: null, name: " M ", priceVnd: 30000 },
        { key: "b", id: null, name: "L", priceVnd: 35000 },
      ],
    };
    const steps = planItemSave(null, form);
    expect(types(steps)).toEqual(["item.create"]);
    expect(steps[0].cmd).toMatchObject({
      sizes: [
        { name: "M", priceVnd: 30000 },
        { name: "L", priceVnd: 35000 },
      ],
    });
  });

  it("orders an edit so the backend never sees an invalid intermediate state", () => {
    let form = itemFormFrom(latte, "");
    form = {
      ...form,
      name: "Latte đá",
      categoryId: "c-tea",
      sizes: [
        { key: "s-m", id: "s-m", name: "Vừa", priceVnd: 41000 },
        { key: "s-l", id: "s-l", name: "L", priceVnd: 45000 },
        { key: "k", id: null, name: "XL", priceVnd: 52000 },
      ],
      description: "",
      image: { kind: "clear" },
      directGroupIds: [],
      excludedGroupIds: ["g-ice"],
    };
    form = removeSizeRow(form, "s-l", retire);
    expect(types(planItemSave(latte, form))).toEqual([
      "item.rename",
      "item.move",
      "size.rename",
      "size.reprice",
      "size.add",
      "size.retire",
      "item.details",
      "item.image.clear",
      "item.groups",
    ]);
  });

  it("reprices a single-price item and needs a PIN for it", () => {
    const steps = planItemSave(tea, { ...itemFormFrom(tea, ""), priceVnd: 47000 });
    expect(steps.map((s) => s.cmd)).toEqual([{ type: "item.reprice", itemId: "i-tea", priceVnd: 47000 }]);
    expect(planNeedsPin(steps)).toBe(true);
  });

  it("does not need a PIN for a rename", () => {
    expect(planNeedsPin(planItemSave(tea, { ...itemFormFrom(tea, ""), name: "Trà đào cam sả" }))).toBe(false);
  });

  it("adopts a new row the server already created when re-planning", () => {
    const form = {
      ...itemFormFrom(latte, ""),
      sizes: [...itemFormFrom(latte, "").sizes, { key: "k", id: null, name: "XL", priceVnd: 52000 }],
    };
    const refreshed: CatItem = { ...latte, sizes: [...latte.sizes, { id: "s-xl", name: "XL", priceVnd: 52000, available: true }] };
    expect(planItemSave(refreshed, form)).toEqual([]);
  });

  it("skips retiring a size the refreshed snapshot no longer has", () => {
    const form = removeSizeRow(itemFormFrom(latte, ""), "s-l", retire);
    const refreshed: CatItem = { ...latte, sizes: [latte.sizes[0]] };
    expect(planItemSave(refreshed, form)).toEqual([]);
  });

  it("clears an image only when one is stored", () => {
    expect(planItemSave(tea, { ...itemFormFrom(tea, ""), image: { kind: "clear" } })).toEqual([]);
  });
});

describe("planCategorySave", () => {
  const tea: CatCategory = { id: "c-tea", name: "Trà", icon: "leaf", displayOrder: 2, groupIds: ["g-ice"] };

  it("creates, then sets details and groups on the new id", () => {
    const steps = planCategorySave(null, { name: "Bánh", icon: "cake", displayOrder: 5, groupIds: ["g-ice"] });
    expect(types(steps)).toEqual(["category.create", "category.details", "category.groups"]);
    expect(steps[1].cmd).toEqual({ type: "category.details", categoryId: NEW_ID, icon: "cake", displayOrder: 5 });
  });

  it("creates alone when icon, order, and groups keep their defaults", () => {
    expect(types(planCategorySave(null, { name: "Bánh", icon: null, displayOrder: 0, groupIds: [] }))).toEqual([
      "category.create",
    ]);
  });

  it("plans only what changed", () => {
    expect(planCategorySave(tea, categoryFormFrom(tea, []))).toEqual([]);
    const steps = planCategorySave(tea, { ...categoryFormFrom(tea, []), displayOrder: 1 });
    expect(steps.map((s) => s.cmd)).toEqual([
      { type: "category.details", categoryId: "c-tea", icon: "leaf", displayOrder: 1 },
    ]);
  });
});

describe("planGroupSave", () => {
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

  it("creates a group in one step with default names", () => {
    const form = {
      ...groupFormFrom(null),
      name: "Mức đá",
      min: 1,
      max: 1,
      rows: [
        { key: "a", id: null, name: "100% đá", surchargeVnd: 0, isDefault: true },
        { key: "b", id: null, name: "Ít đá", surchargeVnd: 0, isDefault: false },
      ],
    };
    expect(planGroupSave(null, form).map((s) => s.cmd)).toEqual([
      {
        type: "group.create",
        name: "Mức đá",
        min: 1,
        max: 1,
        options: [
          { name: "100% đá", surchargeVnd: 0 },
          { name: "Ít đá", surchargeVnd: 0 },
        ],
        defaultOptionNames: ["100% đá"],
      },
    ]);
  });

  it("plans nothing for an unchanged group", () => {
    expect(planGroupSave(topping, groupFormFrom(topping))).toEqual([]);
  });

  it("adds options before the rule and retires after it", () => {
    let form = groupFormFrom(topping);
    form = {
      ...form,
      name: "Topping trà",
      max: 3,
      rows: [
        { ...form.rows[0], name: "Trân châu trắng", surchargeVnd: 12000 },
        form.rows[1],
        { key: "k", id: null, name: "Nha đam", surchargeVnd: 8000, isDefault: true },
      ],
    };
    form = removeOptionRow(form, "o-jelly", retire);
    const steps = planGroupSave(topping, form);
    expect(types(steps)).toEqual([
      "group.rename",
      "option.rename",
      "option.reprice",
      "option.add",
      "group.rule",
      "option.retire",
    ]);
    expect(steps[4].cmd).toEqual({
      type: "group.rule",
      groupId: "g-top",
      min: 0,
      max: 3,
      defaultOptionIds: [`${NEW_OPTION_PREFIX}Nha đam`],
    });
  });

  it("uses the adopted id for a default after re-planning", () => {
    const form = {
      ...groupFormFrom(topping),
      rows: [...groupFormFrom(topping).rows, { key: "k", id: null, name: "Nha đam", surchargeVnd: 8000, isDefault: true }],
    };
    const refreshed: CatGroup = {
      ...topping,
      options: [...topping.options, { id: "o-aloe", name: "Nha đam", surchargeVnd: 8000, available: true }],
    };
    expect(planGroupSave(refreshed, form).map((s) => s.cmd)).toEqual([
      { type: "group.rule", groupId: "g-top", min: 0, max: 2, defaultOptionIds: ["o-jelly", "o-aloe"] },
    ]);
  });
});

describe("single-command plans", () => {
  it("retires each entity kind with its own command", () => {
    expect(types(planRetire("item", "i", "Trà", retire))).toEqual(["item.retire"]);
    expect(types(planRetire("category", "c", "Trà", retire))).toEqual(["category.retire"]);
    expect(types(planRetire("group", "g", "Đá", retire))).toEqual(["group.retire"]);
    expect(planRetire("item", "i", "Trà", retire)[0].label).toBe("Ngừng bán Trà");
  });

  it("applies linker assignments only when they differ", () => {
    const current = { itemIds: ["a"], categoryIds: ["c"] };
    expect(planAssignments("g", "Đá", current, { itemIds: ["a"], categoryIds: ["c"] })).toEqual([]);
    expect(planAssignments("g", "Đá", current, { itemIds: ["a", "b"], categoryIds: [] }).map((s) => s.cmd)).toEqual([
      { type: "group.assignments", groupId: "g", itemIds: ["a", "b"], categoryIds: [] },
    ]);
  });
});
