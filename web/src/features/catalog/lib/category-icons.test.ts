// web/src/features/catalog/lib/category-icons.test.ts
import { describe, expect, it } from "bun:test";
import { LayoutGrid } from "lucide-react";
import { CATEGORY_ICON_NAMES, categoryIcon } from "./category-icons";

describe("category icons", () => {
  it("offers only names the server accepts", () => {
    for (const name of CATEGORY_ICON_NAMES) expect(name).toMatch(/^[a-z0-9-]{1,40}$/);
  });

  it("covers every icon the dev seed uses", () => {
    for (const name of ["coffee", "bean", "leaf", "citrus", "milk", "glass-water", "cherry", "apple", "cup-soda", "ice-cream-bowl", "croissant"]) {
      expect(CATEGORY_ICON_NAMES).toContain(name);
    }
  });

  it("falls back for an unknown or missing icon", () => {
    expect(categoryIcon(null)).toBe(LayoutGrid);
    expect(categoryIcon("unknown")).toBe(LayoutGrid);
  });
});
