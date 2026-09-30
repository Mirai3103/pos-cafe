import { describe, expect, it } from "bun:test";
import {
  changeDue,
  isTenderSufficient,
  suggestTenders,
  latestPaymentChangeDue,
} from "./payment";

describe("changeDue", () => {
  it("returns the difference when the customer overpays", () => {
    expect(changeDue(50_000, 47_000)).toBe(3_000);
  });

  it("returns zero on exact tender", () => {
    expect(changeDue(47_000, 47_000)).toBe(0);
  });

  it("clamps to zero when the tender is short", () => {
    expect(changeDue(40_000, 47_000)).toBe(0);
  });
});

describe("isTenderSufficient", () => {
  it("accepts a tender above the total", () => {
    expect(isTenderSufficient(50_000, 47_000)).toBe(true);
  });

  it("accepts an exact tender", () => {
    expect(isTenderSufficient(47_000, 47_000)).toBe(true);
  });

  it("rejects a short tender", () => {
    expect(isTenderSufficient(46_999, 47_000)).toBe(false);
  });

  it("rejects a total of zero, which is never payable", () => {
    expect(isTenderSufficient(0, 0)).toBe(false);
  });
});

describe("suggestTenders", () => {
  it("offers the exact total then the next notes up", () => {
    expect(suggestTenders(47_000)).toEqual([47_000, 50_000, 100_000, 200_000]);
  });

  it("does not repeat the total when it lands on a note", () => {
    expect(suggestTenders(200_000)).toEqual([200_000, 500_000]);
  });

  it("offers only the total once it exceeds the largest note", () => {
    expect(suggestTenders(600_000)).toEqual([600_000]);
  });

  it("returns nothing for a non-positive total", () => {
    expect(suggestTenders(0)).toEqual([]);
  });
});

describe("latestPaymentChangeDue", () => {
  it("reads the change of the most recent payment", () => {
    expect(
      latestPaymentChangeDue({
        payments: [
          { change_due_vnd: 3_000, received_at: "2026-09-24T02:00:00Z" },
          { change_due_vnd: 7_000, received_at: "2026-09-24T03:00:00Z" },
        ],
      }),
    ).toBe(7_000);
  });

  it("treats a missing change field as zero", () => {
    expect(
      latestPaymentChangeDue({ payments: [{ received_at: "2026-09-24T02:00:00Z" }] }),
    ).toBe(0);
  });

  it("returns zero when there are no payments", () => {
    expect(latestPaymentChangeDue({ payments: [] })).toBe(0);
    expect(latestPaymentChangeDue(null)).toBe(0);
    expect(latestPaymentChangeDue(undefined)).toBe(0);
  });
});
