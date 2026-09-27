import { describe, expect, it } from "bun:test";
import { parseCatalogView } from "./views";

describe("parseCatalogView", () => {
  it("accepts the four views and falls back to items", () => {
    for (const v of ["items", "categories", "groups", "linker"]) expect(parseCatalogView(v)).toBe(v);
    expect(parseCatalogView("nope")).toBe("items");
    expect(parseCatalogView(undefined)).toBe("items");
  });
});
