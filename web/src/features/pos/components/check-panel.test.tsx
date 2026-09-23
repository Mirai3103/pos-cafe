import { describe, it, expect } from "bun:test";
import { renderToString } from "react-dom/server";
import { CheckPanel, type CheckPanelProps } from "./check-panel";
import type { SalesServiceSessionResponse } from "@/api/generated/models";

function session(
  checks: SalesServiceSessionResponse["checks"],
  preparation_units: SalesServiceSessionResponse["preparation_units"] = [],
): SalesServiceSessionResponse {
  return {
    id: "session-1",
    service_number: "007",
    service_mode: "TAKEAWAY",
    state: "ACTIVE",
    sales_shift_id: "shift-1",
    tables: [],
    checks,
    orders: [],
    preparation_units,
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
      submitted: false,
    },
  ],
};

const settledCheck = (submitted: boolean) => ({
  ...openCheck,
  state: "SETTLED",
  balance_vnd: 0,
  total_applied_vnd: 47_000,
  allocations: openCheck.allocations.map((a) => ({ ...a, submitted })),
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
});

const handlers = {
  onCollect: () => {},
  onSubmit: () => {},
  onClose: () => {},
  onNextCustomer: () => {},
  isSubmitting: false,
  isClosing: false,
  submitError: null,
};

function render(props: Pick<CheckPanelProps, "session" | "phase"> & Partial<CheckPanelProps>) {
  return renderToString(<CheckPanel {...handlers} {...props} />);
}

describe("CheckPanel", () => {
  it("lists the committed items and the outstanding balance", () => {
    const html = render({ session: session([openCheck]), phase: "AWAITING_PAYMENT" });
    expect(html).toContain("Đã chốt");
    expect(html).toContain("Cà phê sữa đá");
    expect(html).toContain("Vừa (M)");
    expect(html).toContain("Còn phải thu");
    expect(html).toContain("47.000");
    expect(html).toContain("Thu tiền (F9)");
  });

  it("refuses to collect when more than one Check is open", () => {
    const html = render({
      session: session([openCheck, { ...openCheck, id: "check-2" }]),
      phase: "AWAITING_PAYMENT",
    });
    expect(html).toContain("nhiều hóa đơn");
    expect(html).toContain("disabled");
  });

  it("offers Gửi bếp when paid but not submitted", () => {
    const html = render({ session: session([settledCheck(false)]), phase: "AWAITING_SUBMIT" });
    expect(html).toContain("Chờ gửi bếp");
    expect(html).toContain("Tiền thối");
    expect(html).toContain("3.000");
    expect(html).toContain("Gửi bếp (F9)");
    expect(html).toContain("Khách tiếp theo");
    expect(html).not.toContain("Mở ở Slice 5");
  });

  it("shows the submit failure beside the retry", () => {
    const html = render({
      session: session([settledCheck(false)]),
      phase: "AWAITING_SUBMIT",
      submitError: "Không kết nối được máy chủ.",
    });
    expect(html).toContain("Không kết nối được máy chủ.");
  });

  it("shows progress and a disabled Hoàn tất while the kitchen works", () => {
    const html = render({
      session: session([settledCheck(true)], [{ state: "FULFILLED" }, { state: "QUEUED" }]),
      phase: "IN_PREPARATION",
    });
    expect(html).toContain("Đang pha chế");
    expect(html).toContain("Đã xong 1/2 món");
    expect(html).toContain("Chờ bếp hoàn tất");
    expect(html).toContain("Khách tiếp theo (F9)");
  });

  it("offers Hoàn tất once every unit is terminal", () => {
    const html = render({
      session: session([settledCheck(true)], [{ state: "FULFILLED" }, { state: "FULFILLED" }]),
      phase: "READY_TO_CLOSE",
    });
    expect(html).toContain("Sẵn sàng hoàn tất");
    expect(html).toContain("Đã xong 2/2 món");
    expect(html).toContain("Hoàn tất (F9)");
  });
});
