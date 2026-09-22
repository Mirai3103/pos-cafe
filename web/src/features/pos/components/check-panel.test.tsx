import { describe, it, expect } from "bun:test";
import { renderToString } from "react-dom/server";
import { CheckPanel } from "./check-panel";
import type { SalesServiceSessionResponse } from "@/api/generated/models";

function session(checks: SalesServiceSessionResponse["checks"]): SalesServiceSessionResponse {
  return {
    id: "session-1",
    service_number: "007",
    service_mode: "TAKEAWAY",
    state: "ACTIVE",
    sales_shift_id: "shift-1",
    tables: [],
    checks,
    orders: [],
    preparation_units: [],
  };
}

const openCheck = {
  id: "check-1",
  state: "OPEN",
  charge_vnd: 47_000,
  balance_vnd: 47_000,
  created_at: "2026-09-24T01:00:00Z",
  payments: [],
  allocations: [
    {
      id: "alloc-1",
      name: "Cà phê sữa đá",
      size_name: "Vừa (M)",
      allocated_quantity: 2,
      amount_vnd: 47_000,
      modifiers: [],
    },
  ],
};

describe("CheckPanel", () => {
  it("lists the committed items and the outstanding balance", () => {
    const html = renderToString(
      <CheckPanel
        session={session([openCheck])}
        phase="AWAITING_PAYMENT"
        onCollect={() => {}}
        onNextCustomer={() => {}}
      />,
    );
    expect(html).toContain("Đã chốt");
    expect(html).toContain("Cà phê sữa đá");
    expect(html).toContain("Vừa (M)");
    expect(html).toContain("Còn phải thu");
    expect(html).toContain("47.000");
    expect(html).toContain("Thu tiền (F9)");
  });

  it("shows the settled state with the change given", () => {
    const settled = {
      ...openCheck,
      state: "SETTLED",
      balance_vnd: 0,
      total_applied_vnd: 47_000,
      payments: [
        {
          id: "pay-1",
          method: "CASH",
          applied_amount_vnd: 47_000,
          cash_tendered_vnd: 50_000,
          change_due_vnd: 3_000,
          received_at: "2026-09-24T02:00:00Z",
        },
      ],
    };
    const html = renderToString(
      <CheckPanel
        session={session([settled])}
        phase="SETTLED"
        onCollect={() => {}}
        onNextCustomer={() => {}}
      />,
    );
    expect(html).toContain("Đã thanh toán");
    expect(html).toContain("Tiền thối");
    expect(html).toContain("3.000");
    expect(html).toContain("Khách tiếp theo (F9)");
    expect(html).toContain("Mở ở Slice 5");
  });

  it("refuses to collect when more than one Check is open", () => {
    const html = renderToString(
      <CheckPanel
        session={session([openCheck, { ...openCheck, id: "check-2" }])}
        phase="AWAITING_PAYMENT"
        onCollect={() => {}}
        onNextCustomer={() => {}}
      />,
    );
    expect(html).toContain("nhiều hóa đơn");
  });
});
