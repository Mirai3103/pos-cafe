import { describe, expect, it } from "bun:test";
import {
  getStartingPrice,
  calculateItemUnitPrice,
  calculateLineTotal,
  calculateDraftSubtotal,
} from "./pricing";

describe("pricing calculations", () => {
  describe("getStartingPrice", () => {
    it("returns direct price when item has no sizes", () => {
      const item = { price_vnd: 35000 };
      expect(getStartingPrice(item)).toBe(35000);
    });

    it("returns minimum size price when item has multiple sizes", () => {
      const item = {
        sizes: [
          { price_vnd: 35000 },
          { price_vnd: 25000 },
          { price_vnd: 42000 },
        ],
      };
      expect(getStartingPrice(item)).toBe(25000);
    });

    it("returns 0 when item has neither price nor sizes", () => {
      expect(getStartingPrice({})).toBe(0);
    });

    it("returns direct price when sizes array is empty", () => {
      const item = { price_vnd: 28000, sizes: [] };
      expect(getStartingPrice(item)).toBe(28000);
    });

    it("ignores size prices that are zero or undefined and falls back to item price", () => {
      const item = {
        price_vnd: 30000,
        sizes: [{ price_vnd: 0 }, { price_vnd: undefined }],
      };
      expect(getStartingPrice(item)).toBe(30000);
    });
  });

  describe("calculateItemUnitPrice", () => {
    it("computes base price plus sum of surcharges", () => {
      expect(calculateItemUnitPrice(30000, [5000, 10000])).toBe(45000);
    });

    it("handles zero surcharges", () => {
      expect(calculateItemUnitPrice(25000, [])).toBe(25000);
    });
  });

  describe("calculateLineTotal", () => {
    it("multiplies unit price by quantity", () => {
      expect(calculateLineTotal(45000, 3)).toBe(135000);
    });

    it("clamps minimum quantity to 1", () => {
      expect(calculateLineTotal(45000, 0)).toBe(45000);
      expect(calculateLineTotal(45000, -2)).toBe(45000);
    });
  });

  describe("calculateDraftSubtotal", () => {
    it("computes total across multiple draft items", () => {
      const items = [
        {
          price_vnd: 30000,
          quantity: 2,
          selected_modifier_options: [{ surcharge_vnd: 5000 }],
        },
        {
          price_vnd: 25000,
          quantity: 1,
          selected_modifier_options: [],
        },
      ];
      // Item 1: (30000 + 5000) * 2 = 70000
      // Item 2: 25000 * 1 = 25000
      // Total: 95000
      expect(calculateDraftSubtotal(items)).toBe(95000);
    });

    it("returns 0 for empty or undefined draft items", () => {
      expect(calculateDraftSubtotal([])).toBe(0);
      expect(calculateDraftSubtotal(undefined)).toBe(0);
    });

    it("defaults missing item quantity to 1 and missing item price to 0", () => {
      const items = [
        {
          selected_modifier_options: [{ surcharge_vnd: 5000 }],
        },
      ];
      // Base: 0 + 5000 = 5000; Quantity defaults to 1 -> 5000
      expect(calculateDraftSubtotal(items)).toBe(5000);
    });
  });
});
