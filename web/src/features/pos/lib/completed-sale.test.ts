import { describe, expect, it } from "bun:test";
import {
  completedSaleItems,
  completedSaleTotals,
  completedSalePayments,
  summarizePreparation,
  formatCompletedAt,
} from "./completed-sale";
import type { SalesCompletedSaleResponse } from "@/api/generated/models";

const sale: SalesCompletedSaleResponse = {
  id: "sale-1",
  service_number: "012",
  checks: [
    {
      id: "c1",
      charge_vnd: 47_000,
      effective_received_vnd: 47_000,
      allocations: [{ id: "a1", name: "Cà phê sữa đá" }],
      payments: [
        { id: "p1", method: "CASH", cash_tendered_vnd: 50_000, change_due_vnd: 3_000 },
        { id: "p0", method: "CASH", void: {} },
      ],
    },
    {
      id: "c2",
      charge_vnd: 30_000,
      effective_received_vnd: 30_000,
      allocations: [{ id: "a2", name: "Bạc xỉu" }],
      payments: [],
    },
  ],
};

describe("completed sale readers", () => {
  it("lists the items of every Check", () => {
    expect(completedSaleItems(sale).map((a) => a.name)).toEqual(["Cà phê sữa đá", "Bạc xỉu"]);
  });

  it("sums the server's charge and received figures", () => {
    expect(completedSaleTotals(sale)).toEqual({ chargeVnd: 77_000, receivedVnd: 77_000 });
  });

  it("drops voided payments", () => {
    expect(completedSalePayments(sale).map((p) => p.id)).toEqual(["p1"]);
  });

  it("tolerates a sale with no checks", () => {
    expect(completedSaleItems({})).toEqual([]);
    expect(completedSaleTotals({})).toEqual({ chargeVnd: 0, receivedVnd: 0 });
  });
});

describe("summarizePreparation", () => {
  it("counts each terminal outcome in a fixed order", () => {
    expect(
      summarizePreparation([
        { state: "FULFILLED" },
        { state: "CANCELLED" },
        { state: "FULFILLED" },
        { state: "WASTED" },
      ]),
    ).toBe("2 món đã giao · 1 món hủy · 1 món hỏng");
  });

  it("omits outcomes that did not happen", () => {
    expect(summarizePreparation([{ state: "FULFILLED" }])).toBe("1 món đã giao");
  });

  it("says so when there is nothing", () => {
    expect(summarizePreparation([])).toBe("Không có món pha chế");
    expect(summarizePreparation(undefined)).toBe("Không có món pha chế");
  });
});

describe("formatCompletedAt", () => {
  it("returns an empty string for a missing or invalid time", () => {
    expect(formatCompletedAt(undefined)).toBe("");
    expect(formatCompletedAt("not a date")).toBe("");
  });

  it("formats a valid time", () => {
    expect(formatCompletedAt("2026-09-25T03:04:00Z")).not.toBe("");
  });
});
