import { describe, expect, it } from "bun:test";
import { VND_DENOMINATIONS, formatVND } from "./utils";

// The separator between the digits and the "₫" symbol is a non-breaking
// space (U+00A0), and vi-VN groups thousands with "."; these tests pin the
// exact strings the implementation produces.
describe("formatVND", () => {
  it("formats zero without any grouping", () => {
    expect(formatVND(0)).toBe("0\u00A0₫");
  });

  it("formats a small single-digit amount", () => {
    expect(formatVND(9)).toBe("9\u00A0₫");
  });

  it("groups thousands with dots", () => {
    expect(formatVND(15000)).toBe("15.000\u00A0₫");
  });

  it("groups millions with repeated dots", () => {
    expect(formatVND(1234567)).toBe("1.234.567\u00A0₫");
  });

  it("formats a large nine-digit amount without scientific notation", () => {
    expect(formatVND(999999999)).toBe("999.999.999\u00A0₫");
  });
});

describe("VND_DENOMINATIONS", () => {
  it("lists every Vietnamese note in descending order", () => {
    expect([...VND_DENOMINATIONS]).toEqual([
      500_000, 200_000, 100_000, 50_000, 20_000, 10_000, 5_000, 2_000, 1_000,
    ]);
  });
});
