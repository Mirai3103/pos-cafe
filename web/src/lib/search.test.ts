import { describe, expect, it } from "bun:test";
import { matchesSearch, normalizeVietnamese } from "./search";

describe("lib/search", () => {
  it("strips diacritics and d-stroke", () => {
    expect(normalizeVietnamese("  Bạc  Xỉu Đá ")).toBe("bac xiu da");
  });

  it("matches without diacritics and by cafe acronym", () => {
    expect(matchesSearch("ca phe sua", "Cà phê sữa đá")).toBe(true);
    expect(matchesSearch("cfsd", "Cà phê sữa đá")).toBe(true);
    expect(matchesSearch("tra", "Cà phê sữa đá")).toBe(false);
  });
});
