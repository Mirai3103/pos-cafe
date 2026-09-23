import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { CompletedSaleDialog } from "./completed-sale-dialog";
import type { SalesCompletedSaleResponse } from "@/api/generated/models";

const sale: SalesCompletedSaleResponse = {
  id: "sale-1",
  service_number: "012",
  completed_at: "2026-09-25T03:04:00Z",
  completed_by_display_name: "Thu Ngan A",
  checks: [
    {
      id: "c1",
      charge_vnd: 47_000,
      effective_received_vnd: 47_000,
      allocations: [
        { id: "a1", name: "Cà phê sữa đá", allocated_quantity: 2, amount_vnd: 47_000 },
      ],
      payments: [{ id: "p1", method: "CASH", cash_tendered_vnd: 50_000, change_due_vnd: 3_000 }],
    },
  ],
  preparation_units: [{ state: "FULFILLED" }, { state: "FULFILLED" }],
};

describe("CompletedSaleDialog", () => {
  it("renders nothing without a sale", () => {
    expect(renderToString(<CompletedSaleDialog sale={null} onDone={() => {}} />)).toBe("");
  });

  it("shows the frozen record of the sale", () => {
    const html = renderToString(<CompletedSaleDialog sale={sale} onDone={() => {}} />);
    expect(html).toContain("Đơn #012 đã hoàn tất");
    expect(html).toContain("Thu Ngan A");
    expect(html).toContain("Cà phê sữa đá");
    expect(html).toContain("47.000");
    expect(html).toContain("50.000");
    expect(html).toContain("3.000");
    expect(html).toContain("2 món đã giao");
    expect(html).toContain("Xong (Enter)");
  });
});
