import { describe, expect, it } from "bun:test";
import {
  MAX_PREPARATION_NOTE_LENGTH,
  resolveInitialSelection,
  toggleModifierOption,
  isSelectionValid,
  normalizePreparationNote,
  matchesDraftItemConfig,
  diffDraftItemEdits,
} from "./selection";
import type {
  CatalogSellableItemResponse,
  CatalogSellableModifierGroupResponse,
} from "@/api/generated/models";

describe("selection utilities", () => {
  const sampleGroupSugar: CatalogSellableModifierGroupResponse = {
    id: "group-sugar",
    name: "Muc duong",
    min_selections: 1,
    max_selections: 1,
    default_option_ids: ["opt-sugar-100"],
    options: [
      { id: "opt-sugar-100", name: "100% duong", surcharge_vnd: 0 },
      { id: "opt-sugar-50", name: "50% duong", surcharge_vnd: 0 },
    ],
  };

  const sampleGroupTopping: CatalogSellableModifierGroupResponse = {
    id: "group-topping",
    name: "Topping",
    min_selections: 0,
    max_selections: 2,
    default_option_ids: [],
    options: [
      { id: "opt-pearl", name: "Tran chau", surcharge_vnd: 5000 },
      { id: "opt-jelly", name: "Thach dua", surcharge_vnd: 5000 },
      { id: "opt-pudding", name: "Pudding", surcharge_vnd: 7000 },
    ],
  };

  const sampleItem: CatalogSellableItemResponse = {
    id: "item-coffee",
    name: "Ca phe sua da",
    sizes: [
      { id: "size-s", name: "Size S", price_vnd: 29000 },
      { id: "size-m", name: "Size M", price_vnd: 35000 },
    ],
    modifier_groups: [sampleGroupSugar, sampleGroupTopping],
  };

  describe("resolveInitialSelection", () => {
    it("pre-selects first size and default options", () => {
      const initial = resolveInitialSelection(sampleItem);
      expect(initial.sizeId).toBe("size-s");
      expect(initial.selectedOptionIds).toEqual(["opt-sugar-100"]);
      expect(initial.note).toBe("");
      expect(initial.quantity).toBe(1);
    });

    it("handles item with no sizes or modifiers", () => {
      const simpleItem: CatalogSellableItemResponse = {
        id: "item-croissant",
        name: "Croissant",
        price_vnd: 35000,
      };
      const initial = resolveInitialSelection(simpleItem);
      expect(initial.sizeId).toBeUndefined();
      expect(initial.selectedOptionIds).toEqual([]);
      expect(initial.note).toBe("");
      expect(initial.quantity).toBe(1);
    });

    it("handles item with sizes but no modifier groups", () => {
      const itemWithSizes: CatalogSellableItemResponse = {
        id: "item-tea",
        name: "Tra dao",
        sizes: [{ id: "size-regular", name: "Regular", price_vnd: 30000 }],
      };
      const initial = resolveInitialSelection(itemWithSizes);
      expect(initial.sizeId).toBe("size-regular");
      expect(initial.selectedOptionIds).toEqual([]);
    });

    it("handles modifier groups without default options", () => {
      const groupWithoutDefaults: CatalogSellableModifierGroupResponse = {
        id: "group-ice",
        name: "Da",
        min_selections: 0,
        max_selections: 1,
        options: [{ id: "opt-ice-normal", name: "Binh thuong", surcharge_vnd: 0 }],
      };
      const item: CatalogSellableItemResponse = {
        id: "item-juice",
        name: "Nuoc cam",
        modifier_groups: [groupWithoutDefaults],
      };
      const initial = resolveInitialSelection(item);
      expect(initial.selectedOptionIds).toEqual([]);
    });
  });

  describe("toggleModifierOption", () => {
    it("enforces radio replacement when max_selections is 1", () => {
      const current = ["opt-sugar-100"];
      const next = toggleModifierOption(
        "group-sugar",
        "opt-sugar-50",
        current,
        sampleGroupSugar,
      );
      expect(next).toEqual(["opt-sugar-50"]);
    });

    it("adds option to multi-select group when below max_selections", () => {
      const current = ["opt-sugar-100", "opt-pearl"];
      const next = toggleModifierOption(
        "group-topping",
        "opt-jelly",
        current,
        sampleGroupTopping,
      );
      expect(next).toEqual(["opt-sugar-100", "opt-pearl", "opt-jelly"]);
    });

    it("unselects an already selected option in multi-select group", () => {
      const current = ["opt-sugar-100", "opt-pearl", "opt-jelly"];
      const next = toggleModifierOption(
        "group-topping",
        "opt-pearl",
        current,
        sampleGroupTopping,
      );
      expect(next).toEqual(["opt-sugar-100", "opt-jelly"]);
    });

    it("blocks adding option when max_selections is reached in multi-select group", () => {
      const current = ["opt-pearl", "opt-jelly"];
      const next = toggleModifierOption(
        "group-topping",
        "opt-pudding",
        current,
        sampleGroupTopping,
      );
      // Reached max 2: retains current without adding
      expect(next).toEqual(["opt-pearl", "opt-jelly"]);
    });

    it("preserves options from other modifier groups during toggles", () => {
      const current = ["opt-other-group", "opt-pearl"];
      const next = toggleModifierOption(
        "group-topping",
        "opt-jelly",
        current,
        sampleGroupTopping,
      );
      expect(next).toEqual(["opt-other-group", "opt-pearl", "opt-jelly"]);
    });
  });

  describe("isSelectionValid", () => {
    it("returns true when size is chosen and min_selections is satisfied", () => {
      expect(isSelectionValid(sampleItem, "size-m", ["opt-sugar-100"])).toBe(true);
    });

    it("returns false when item has sizes but none is selected", () => {
      expect(isSelectionValid(sampleItem, undefined, ["opt-sugar-100"])).toBe(false);
    });

    it("returns false when a required modifier group has no selection", () => {
      expect(isSelectionValid(sampleItem, "size-m", [])).toBe(false);
    });

    it("returns true when item has no sizes and no modifier groups", () => {
      const simpleItem: CatalogSellableItemResponse = {
        id: "item-croissant",
        name: "Croissant",
        price_vnd: 35000,
      };
      expect(isSelectionValid(simpleItem, undefined, [])).toBe(true);
    });

    it("returns true when modifier groups have min_selections 0 and none selected", () => {
      const optionalGroupItem: CatalogSellableItemResponse = {
        id: "item-water",
        name: "Nuoc suoi",
        modifier_groups: [sampleGroupTopping],
      };
      expect(isSelectionValid(optionalGroupItem, undefined, [])).toBe(true);
    });
  });

  describe("normalizePreparationNote", () => {
    it("trims whitespace", () => {
      expect(normalizePreparationNote("  it da  ")).toBe("it da");
    });

    it("truncates note to 200 unicode characters", () => {
      const longNote = "a".repeat(250);
      const normalized = normalizePreparationNote(longNote);
      expect(normalized.length).toBe(MAX_PREPARATION_NOTE_LENGTH);
    });

    it("handles unicode text and preserves characters within limit", () => {
      const unicodeText = "It duong, nhieu da, giao tan noi";
      expect(normalizePreparationNote(`  ${unicodeText}  `)).toBe(unicodeText);
    });

    it("correctly counts and slices 200 unicode code points", () => {
      const unicodeChar = "\u0111"; // Vietnamese letter 'd' with stroke
      const longUnicode = unicodeChar.repeat(250);
      const normalized = normalizePreparationNote(longUnicode);
      expect(Array.from(normalized).length).toBe(200);
      expect(normalized).toBe(unicodeChar.repeat(200));
    });
  });

  describe("matchesDraftItemConfig", () => {
    const baseItem = {
      menu_item_id: "cf-1",
      size_id: "sz-m",
      preparation_note: "it da",
      selected_modifier_options: [{ id: "opt-1" }, { id: "opt-2" }],
    };

    it("matches identical configuration", () => {
      expect(
        matchesDraftItemConfig(baseItem, "cf-1", {
          sizeId: "sz-m",
          preparationNote: "it da",
          selectedOptionIds: ["opt-1", "opt-2"],
        }),
      ).toBe(true);
    });

    it("matches irrespective of option array sorting", () => {
      expect(
        matchesDraftItemConfig(baseItem, "cf-1", {
          sizeId: "sz-m",
          preparationNote: "it da",
          selectedOptionIds: ["opt-2", "opt-1"],
        }),
      ).toBe(true);
    });

    it("does not match when menu item id differs", () => {
      expect(
        matchesDraftItemConfig(baseItem, "cf-2", {
          sizeId: "sz-m",
          preparationNote: "it da",
          selectedOptionIds: ["opt-1", "opt-2"],
        }),
      ).toBe(false);
    });

    it("does not match when size differs", () => {
      expect(
        matchesDraftItemConfig(baseItem, "cf-1", {
          sizeId: "sz-l",
          preparationNote: "it da",
          selectedOptionIds: ["opt-1", "opt-2"],
        }),
      ).toBe(false);
    });

    it("does not match when note differs", () => {
      expect(
        matchesDraftItemConfig(baseItem, "cf-1", {
          sizeId: "sz-m",
          preparationNote: "nhieu da",
          selectedOptionIds: ["opt-1", "opt-2"],
        }),
      ).toBe(false);
    });

    it("does not match when option ids differ", () => {
      expect(
        matchesDraftItemConfig(baseItem, "cf-1", {
          sizeId: "sz-m",
          preparationNote: "it da",
          selectedOptionIds: ["opt-1", "opt-3"],
        }),
      ).toBe(false);
    });
  });

  describe("diffDraftItemEdits", () => {
    const initial = {
      sizeId: "sz-m",
      selectedOptionIds: ["opt-1", "opt-2"],
      preparationNote: "it da",
      quantity: 2,
    };

    it("detects no changes when configuration matches initial values", () => {
      const diff = diffDraftItemEdits(initial, {
        sizeId: "sz-m",
        selectedOptionIds: ["opt-2", "opt-1"], // same options, different order
        preparationNote: "it da",
        quantity: 2,
      });

      expect(diff.quantityChanged).toBe(false);
      expect(diff.modifiersChanged).toBe(false);
      expect(diff.noteChanged).toBe(false);
      expect(diff.sizeChanged).toBe(false);
    });

    it("detects quantity change", () => {
      const diff = diffDraftItemEdits(initial, {
        ...initial,
        quantity: 3,
      });

      expect(diff.quantityChanged).toBe(true);
      expect(diff.modifiersChanged).toBe(false);
      expect(diff.noteChanged).toBe(false);
      expect(diff.sizeChanged).toBe(false);
    });

    it("detects modifier changes when options are added, removed, or replaced", () => {
      const diffAdded = diffDraftItemEdits(initial, {
        ...initial,
        selectedOptionIds: ["opt-1", "opt-2", "opt-3"],
      });
      expect(diffAdded.modifiersChanged).toBe(true);

      const diffRemoved = diffDraftItemEdits(initial, {
        ...initial,
        selectedOptionIds: ["opt-1"],
      });
      expect(diffRemoved.modifiersChanged).toBe(true);

      const diffReplaced = diffDraftItemEdits(initial, {
        ...initial,
        selectedOptionIds: ["opt-1", "opt-4"],
      });
      expect(diffReplaced.modifiersChanged).toBe(true);
    });

    it("detects preparation note change", () => {
      const diff = diffDraftItemEdits(initial, {
        ...initial,
        preparationNote: "khong duong",
      });

      expect(diff.quantityChanged).toBe(false);
      expect(diff.modifiersChanged).toBe(false);
      expect(diff.noteChanged).toBe(true);
      expect(diff.sizeChanged).toBe(false);
    });

    it("detects size change when different sizeId is specified", () => {
      const diff = diffDraftItemEdits(initial, {
        ...initial,
        sizeId: "sz-l",
      });

      expect(diff.quantityChanged).toBe(false);
      expect(diff.modifiersChanged).toBe(false);
      expect(diff.noteChanged).toBe(false);
      expect(diff.sizeChanged).toBe(true);
    });

    it("does not report sizeChanged when sizeId is omitted/undefined", () => {
      const diff = diffDraftItemEdits(initial, {
        ...initial,
        sizeId: undefined,
      });

      expect(diff.sizeChanged).toBe(false);
    });
  });
});
