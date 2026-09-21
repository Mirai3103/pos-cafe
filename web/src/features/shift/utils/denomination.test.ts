import { describe, expect, it } from "bun:test";
import {
  VND_DENOMINATIONS,
  calculateDenominationTotal,
  formatDenomination,
} from "./denomination";

describe("denomination utilities", () => {
  it("lists all 9 Vietnamese Dong denominations", () => {
    expect(VND_DENOMINATIONS).toEqual([
      500_000, 200_000, 100_000, 50_000, 20_000, 10_000, 5_000, 2_000, 1_000,
    ]);
  });

  it("calculates total from count mapping correctly", () => {
    const counts = {
      500_000: 2, // 1,000,000
      100_000: 3, // 300,000
      50_000: 1,  // 50,000
      1_000: 5,   // 5,000
    };
    expect(calculateDenominationTotal(counts)).toBe(1_355_000);
  });

  it("ignores negative or null counts", () => {
    const counts = {
      500_000: -1,
      200_000: 1,
    };
    expect(calculateDenominationTotal(counts)).toBe(200_000);
  });

  it("formats denominations cleanly", () => {
    expect(formatDenomination(500_000)).toBe("500.000 đ");
    expect(formatDenomination(20_000)).toBe("20.000 đ");
  });
});
