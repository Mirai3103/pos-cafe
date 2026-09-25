import { describe, expect, it } from "bun:test";
import type { CatalogAvailabilityMenuResponse } from "@/api/generated/models";
import {
  ALL_SCOPE,
  EMPTY_FILTER,
  TOPPINGS_SCOPE,
  applyAvailability,
  blockedMessage,
  filterGroups,
  filterItems,
  nextIntent,
  refsKey,
  toAvailabilityView,
  toRestoreChanges,
} from "./availability";

const sugar = (a100: boolean, a50: boolean) => ({
  id: "g-sugar",
  name: "Mức đường",
  min_selections: 1,
  max_selections: 1,
  options: [
    { id: "o-100", name: "100% đường", available: a100 },
    { id: "o-50", name: "50% đường", available: a50 },
  ],
});

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
            { id: "s-den-s", name: "Size S", available: true },
            { id: "s-den-l", name: "Size L", available: false },
          ],
          modifier_groups: [sugar(true, true), topping],
        },
        {
          id: "i-sua",
          name: "Cà phê sữa đá",
          category_id: "c-coffee",
          available: false,
          modifier_groups: [sugar(true, true), topping],
        },
      ],
    },
    {
      id: "c-cake",
      name: "Bánh ngọt",
      items: [{ id: "i-croissant", name: "Croissant bơ tỏi", category_id: "c-cake", available: true }],
    },
  ],
};

describe("toAvailabilityView", () => {
  const view = toAvailabilityView(menu);

  it("flattens items with their category", () => {
    expect(view.items.map((i) => i.id)).toEqual(["i-den", "i-sua", "i-croissant"]);
    expect(view.items[2].categoryName).toBe("Bánh ngọt");
    expect(view.categories).toEqual([
      { id: "c-coffee", name: "Cà phê" },
      { id: "c-cake", name: "Bánh ngọt" },
    ]);
  });

  it("deduplicates modifier groups and options repeated under each item", () => {
    expect(view.groups.map((g) => g.id)).toEqual(["g-sugar", "g-top"]);
    expect(view.groups[0].options.map((o) => o.id)).toEqual(["o-100", "o-50"]);
    expect(view.groups[1].options[0].groupName).toBe("Topping thêm");
  });

  it("counts every item, size, and option once", () => {
    // 3 items + 2 sizes + 3 options = 8; off: i-sua, s-den-l, o-pearl
    expect(view.stats).toEqual({ total: 8, available: 5, unavailable: 3 });
    expect(view.unavailableRefs).toEqual([
      { kind: "size", id: "s-den-l", name: "Cà phê đen (Size L)" },
      { kind: "item", id: "i-sua", name: "Cà phê sữa đá" },
      { kind: "modifier_option", id: "o-pearl", name: "Trân châu trắng (Topping thêm)" },
    ]);
  });

  it("is empty for a missing menu", () => {
    expect(toAvailabilityView(null).stats).toEqual({ total: 0, available: 0, unavailable: 0 });
  });
});

describe("blockedBy", () => {
  it("flags a required group with fewer available options than its minimum", () => {
    const blocked = toAvailabilityView({
      categories: [{ id: "c", name: "C", items: [{ id: "i", name: "I", available: true, modifier_groups: [sugar(false, false)] }] }],
    });
    expect(blocked.items[0].blockedBy).toEqual(["Mức đường"]);
  });

  it("flags a sized item whose sizes are all off", () => {
    const blocked = toAvailabilityView({
      categories: [{ id: "c", name: "C", items: [{ id: "i", name: "I", available: true, sizes: [{ id: "s", name: "S", available: false }] }] }],
    });
    expect(blocked.items[0].blockedBy).toEqual(["Kích cỡ"]);
  });

  it("does not flag an optional group", () => {
    expect(toAvailabilityView(menu).items[0].blockedBy).toEqual([]);
  });

  it("formats the warning", () => {
    expect(blockedMessage([])).toBeNull();
    expect(blockedMessage(["Mức đường"])).toBe("Không bán được: hết tùy chọn bắt buộc (Mức đường)");
    expect(blockedMessage(["Kích cỡ", "Mức đường", "Đá"])).toBe(
      "Không bán được: hết kích cỡ; hết tùy chọn bắt buộc (Mức đường, Đá)",
    );
  });
});

describe("filters", () => {
  const view = toAvailabilityView(menu);

  it("returns everything with the empty filter", () => {
    expect(filterItems(view.items, EMPTY_FILTER)).toHaveLength(3);
    expect(filterGroups(view.groups, EMPTY_FILTER)).toHaveLength(2);
  });

  it("searches without diacritics across item, size, and category names", () => {
    expect(filterItems(view.items, { ...EMPTY_FILTER, query: "ca phe sua" }).map((i) => i.id)).toEqual(["i-sua"]);
    expect(filterItems(view.items, { ...EMPTY_FILTER, query: "banh" }).map((i) => i.id)).toEqual(["i-croissant"]);
  });

  it("scopes to one category, hiding toppings", () => {
    const f = { ...EMPTY_FILTER, scope: "c-cake" };
    expect(filterItems(view.items, f).map((i) => i.id)).toEqual(["i-croissant"]);
    expect(filterGroups(view.groups, f)).toEqual([]);
  });

  it("scopes to toppings, hiding items", () => {
    const f = { ...EMPTY_FILTER, scope: TOPPINGS_SCOPE };
    expect(filterItems(view.items, f)).toEqual([]);
    expect(filterGroups(view.groups, f)).toHaveLength(2);
  });

  it("keeps only items or options with something off", () => {
    const f = { ...EMPTY_FILTER, scope: ALL_SCOPE, onlyUnavailable: true };
    expect(filterItems(view.items, f).map((i) => i.id)).toEqual(["i-den", "i-sua"]);
    expect(filterGroups(view.groups, f).map((g) => g.id)).toEqual(["g-top"]);
  });
});

describe("applyAvailability", () => {
  it("patches every occurrence of an option without mutating the input", () => {
    const next = applyAvailability(menu, "modifier_option", "o-pearl", true);
    const options = next.categories!.flatMap((c) => c.items ?? []).flatMap((i) => i.modifier_groups ?? []).flatMap((g) => g.options ?? []);
    expect(options.filter((o) => o.id === "o-pearl").every((o) => o.available)).toBe(true);
    expect(menu.categories![0].items![0].modifier_groups![1].options![0].available).toBe(false);
  });

  it("patches an item and a size", () => {
    const next = applyAvailability(applyAvailability(menu, "item", "i-sua", true), "size", "s-den-l", true);
    const view = toAvailabilityView(next);
    expect(view.unavailableRefs.map((r) => r.id)).toEqual(["o-pearl"]);
  });
});

describe("restore helpers", () => {
  const refs = toAvailabilityView(menu).unavailableRefs;

  it("turns every ref on", () => {
    expect(toRestoreChanges(refs)).toEqual([
      { kind: "size", id: "s-den-l", available: true },
      { kind: "item", id: "i-sua", available: true },
      { kind: "modifier_option", id: "o-pearl", available: true },
    ]);
  });

  it("keeps the intent id while the list is unchanged and renews it when the list changes", () => {
    const first = nextIntent(null, refsKey(refs));
    expect(nextIntent(first, refsKey(refs))).toBe(first);
    const changed = nextIntent(first, refsKey(refs.slice(1)));
    expect(changed.id).not.toBe(first.id);
  });
});
