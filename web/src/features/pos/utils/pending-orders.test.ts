import { describe, expect, it } from "bun:test";
import {
  toPendingOrders,
  countReadyToClose,
  summarizeItems,
  minutesSince,
  formatAge,
} from "./pending-orders";
import type { SalesServiceSessionResponse } from "@/api/generated/models";

function s(overrides: Partial<SalesServiceSessionResponse>): SalesServiceSessionResponse {
  return {
    id: "s",
    service_number: "000",
    service_mode: "TAKEAWAY",
    state: "ACTIVE",
    checks: [],
    preparation_units: [],
    ...overrides,
  };
}

const paid = (submitted: boolean) => [
  {
    id: "c",
    state: "SETTLED",
    charge_vnd: 47_000,
    allocations: [{ id: "a", name: "Cà phê sữa đá", submitted }],
  },
];

describe("toPendingOrders", () => {
  it("keeps takeaway sessions only", () => {
    const orders = toPendingOrders([
      s({ id: "t", service_mode: "TAKEAWAY" }),
      s({ id: "d", service_mode: "DINE_IN" }),
    ]);
    expect(orders.map((o) => o.sessionId)).toEqual(["t"]);
  });

  it("puts ready-to-close first, then awaiting-submit, then the rest oldest first", () => {
    const orders = toPendingOrders([
      s({ id: "drafting-old", created_at: "2026-09-25T01:00:00Z", draft: { state: "EDITABLE", items: [] } }),
      s({
        id: "cooking",
        created_at: "2026-09-25T00:30:00Z",
        checks: paid(true),
        preparation_units: [{ state: "QUEUED" }],
      }),
      s({ id: "unsent", created_at: "2026-09-25T02:00:00Z", checks: paid(false) }),
      s({
        id: "ready",
        created_at: "2026-09-25T03:00:00Z",
        checks: paid(true),
        preparation_units: [{ state: "FULFILLED" }],
      }),
    ]);
    expect(orders.map((o) => o.sessionId)).toEqual(["ready", "unsent", "cooking", "drafting-old"]);
    expect(orders.map((o) => o.phase)).toEqual([
      "READY_TO_CLOSE",
      "AWAITING_SUBMIT",
      "IN_PREPARATION",
      "DRAFTING",
    ]);
  });

  it("totals a committed session from its Checks and summarizes its items", () => {
    const [order] = toPendingOrders([s({ id: "x", checks: paid(false) })]);
    expect(order.totalVnd).toBe(47_000);
    expect(order.itemSummary).toBe("Cà phê sữa đá");
  });

  it("tolerates a missing list", () => {
    expect(toPendingOrders(undefined)).toEqual([]);
  });

  it("counts the orders ready to close", () => {
    const orders = toPendingOrders([
      s({ id: "a", checks: paid(true), preparation_units: [{ state: "FULFILLED" }] }),
      s({ id: "b", checks: paid(false) }),
    ]);
    expect(countReadyToClose(orders)).toBe(1);
  });
});

describe("summarizeItems", () => {
  it("names up to two items, then counts the rest", () => {
    expect(summarizeItems(["A", "B", "C", "D"])).toBe("A, B +2 món");
    expect(summarizeItems(["A", "B"])).toBe("A, B");
    expect(summarizeItems([])).toBe("Chưa có món");
    expect(summarizeItems(["", ""])).toBe("Chưa có món");
  });
});

describe("age", () => {
  it("counts whole minutes and never goes negative", () => {
    const now = Date.parse("2026-09-25T01:10:30Z");
    expect(minutesSince("2026-09-25T01:00:00Z", now)).toBe(10);
    expect(minutesSince("2026-09-25T02:00:00Z", now)).toBe(0);
    expect(minutesSince("garbage", now)).toBe(0);
  });

  it("words the age", () => {
    expect(formatAge(0)).toBe("Vừa xong");
    expect(formatAge(7)).toBe("7 phút trước");
  });
});
