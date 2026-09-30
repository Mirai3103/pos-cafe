import { describe, expect, it } from "bun:test";
import {
  normalizeVietnamese,
  getAcronym,
  matchesSearch,
  filterSellableItems,
} from "./search";
import type { CatalogSellableCategoryResponse } from "@/api/generated/models";

describe("search utilities", () => {
  describe("normalizeVietnamese", () => {
    it("strips diacritics and converts to lowercase", () => {
      expect(normalizeVietnamese("Cà phê Sữa Đá")).toBe("ca phe sua da");
      expect(normalizeVietnamese("Trà Đào Cam Sả")).toBe("tra dao cam sa");
    });

    it("handles complex Vietnamese diacritics and d with stroke", () => {
      expect(normalizeVietnamese("Ẩm thực")).toBe("am thuc");
      expect(normalizeVietnamese("Lễ hội")).toBe("le hoi");
      expect(normalizeVietnamese("Ớt hiểm")).toBe("ot hiem");
      expect(normalizeVietnamese("Vực thẳm")).toBe("vuc tham");
      expect(normalizeVietnamese("Đà Nẵng")).toBe("da nang");
      expect(normalizeVietnamese("đường")).toBe("duong");
    });

    it("handles irregular whitespace and trims input", () => {
      expect(normalizeVietnamese("   Cà   phê    sữa   đá   ")).toBe("ca phe sua da");
      expect(normalizeVietnamese("")).toBe("");
    });
  });

  describe("getAcronym", () => {
    it("extracts first letter of each word in lowercase", () => {
      expect(getAcronym("Cà phê sữa đá")).toBe("cfsd");
      expect(getAcronym("Bạc xỉu")).toBe("bx");
      expect(getAcronym("Trà đào cam sả")).toBe("tdcs");
    });

    it("handles cafe convention for ca phe items", () => {
      expect(getAcronym("Cà phê")).toBe("cf");
      expect(getAcronym("Cà phê đen")).toBe("cfd");
      expect(getAcronym("Cà phê đen đá")).toBe("cfdd");
      expect(getAcronym("Cà phê muối")).toBe("cfm");
    });

    it("handles items with punctuation and special formatting", () => {
      expect(getAcronym("Croissant (bơ tỏi)")).toBe("cbt");
      expect(getAcronym("Trà sữa - Trân châu")).toBe("tstc");
      expect(getAcronym("Matcha Latte / Nóng")).toBe("mln");
    });

    it("handles edge cases such as empty string or whitespace", () => {
      expect(getAcronym("")).toBe("");
      expect(getAcronym("   ")).toBe("");
      expect(getAcronym("---")).toBe("");
    });
  });

  describe("matchesSearch", () => {
    it("matches exact substring in normalized item name", () => {
      expect(matchesSearch("ca phe", "Cà phê Đen")).toBe(true);
      expect(matchesSearch("den", "Cà phê Đen")).toBe(true);
      expect(matchesSearch("ĐEN", "Cà phê Đen")).toBe(true);
    });

    it("matches acronym query", () => {
      expect(matchesSearch("cfsd", "Cà phê sữa đá")).toBe(true);
      expect(matchesSearch("bx", "Bạc xỉu đá")).toBe(true);
      expect(matchesSearch("tdcs", "Trà đào cam sả")).toBe(true);
    });

    it("matches direct initial acronym as alternative for ca phe", () => {
      expect(matchesSearch("cpsd", "Cà phê sữa đá")).toBe(true);
    });

    it("matches category name if provided", () => {
      expect(matchesSearch("banh", "Croissant bơ tỏi", "Bánh ngọt")).toBe(true);
      expect(matchesSearch("bn", "Croissant bơ tỏi", "Bánh ngọt")).toBe(true);
    });

    it("returns true on empty query", () => {
      expect(matchesSearch("", "Bất kỳ món nào")).toBe(true);
      expect(matchesSearch("   ", "Bất kỳ món nào")).toBe(true);
    });

    it("matches queries with irregular whitespace and punctuation", () => {
      expect(matchesSearch("  cfsd  ", "Cà phê sữa đá")).toBe(true);
      expect(matchesSearch("croissant bo toi", "Croissant (bơ tỏi)")).toBe(true);
      expect(matchesSearch("tra sua", "Trà sữa - Trân châu")).toBe(true);
    });

    it("returns false when query does not match", () => {
      expect(matchesSearch("matcha", "Cà phê sữa đá", "Cà phê")).toBe(false);
      expect(matchesSearch("tdcs", "Bạc xỉu", "Cà phê")).toBe(false);
    });
  });

  describe("filterSellableItems", () => {
    const categories: CatalogSellableCategoryResponse[] = [
      {
        id: "cat-1",
        name: "Cà phê",
        items: [
          { id: "item-1", name: "Cà phê sữa đá", price_vnd: 29000 },
          { id: "item-2", name: "Bạc xỉu", price_vnd: 32000 },
        ],
      },
      {
        id: "cat-2",
        name: "Bánh ngọt",
        items: [{ id: "item-3", name: "Croissant bơ tỏi", price_vnd: 35000 }],
      },
    ];

    it("filters by category", () => {
      const items = filterSellableItems(categories, "cat-2", "");
      expect(items.length).toBe(1);
      expect(items[0].name).toBe("Croissant bơ tỏi");
    });

    it("returns all items when selectedCategoryId is null", () => {
      const items = filterSellableItems(categories, null, "");
      expect(items.length).toBe(3);
    });

    it("filters across categories with acronym query", () => {
      const items = filterSellableItems(categories, null, "bx");
      expect(items.length).toBe(1);
      expect(items[0].name).toBe("Bạc xỉu");
    });

    it("filters within a category with substring query", () => {
      const items = filterSellableItems(categories, "cat-1", "sua");
      expect(items.length).toBe(1);
      expect(items[0].name).toBe("Cà phê sữa đá");
    });

    it("handles categories with undefined or empty items", () => {
      const emptyCat: CatalogSellableCategoryResponse[] = [
        { id: "cat-empty", name: "Trống", items: [] },
        { id: "cat-undef", name: "Không có", items: undefined },
      ];
      expect(filterSellableItems(emptyCat, null, "")).toEqual([]);
    });

    it("returns empty array when selectedCategoryId does not match", () => {
      const items = filterSellableItems(categories, "cat-unknown", "");
      expect(items.length).toBe(0);
    });
  });
});
