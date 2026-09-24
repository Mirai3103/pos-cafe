import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { PendingOrdersList, PendingOrdersButton } from "./pending-orders-drawer";
import type { PendingOrder } from "../utils/pending-orders";

// The Sheet shell renders through a portal, which renderToString leaves empty,
// so these tests cover the list body the Sheet wraps.

const order = (overrides: Partial<PendingOrder>): PendingOrder => ({
  sessionId: "s1",
  serviceNumber: "012",
  createdAt: "2026-09-25T01:00:00Z",
  phase: "IN_PREPARATION",
  progress: { done: 1, total: 3 },
  totalVnd: 47_000,
  itemSummary: "Cà phê sữa đá",
  tableLabel: null,
  ...overrides,
});

const base = {
  orders: [] as PendingOrder[],
  isLoading: false,
  errorMessage: null,
  activeSessionId: null,
  nowMs: Date.parse("2026-09-25T01:05:00Z"),
  onSelect: () => {},
  onRetry: () => {},
};

describe("PendingOrdersList", () => {
  it("shows skeleton rows while loading", () => {
    const html = renderToString(<PendingOrdersList {...base} isLoading />);
    expect(html).toContain("animate-pulse");
    expect(html).not.toContain("Không có đơn nào đang chờ");
  });

  it("says so when nothing is waiting", () => {
    expect(renderToString(<PendingOrdersList {...base} />)).toContain(
      "Không có đơn nào đang chờ",
    );
  });

  it("renders each order with status, progress, total and age", () => {
    const html = renderToString(
      <PendingOrdersList
        {...base}
        orders={[order({}), order({ sessionId: "s2", serviceNumber: "013", phase: "AWAITING_SUBMIT" })]}
        activeSessionId="s2"
      />,
    );
    expect(html).toContain("#012");
    expect(html).toContain("Đang pha chế");
    expect(html).toContain("1/3 món xong");
    expect(html).toContain("47.000");
    expect(html).toContain("5 phút trước");
    expect(html).toContain("#013");
    expect(html).toContain("Chờ gửi bếp");
    expect(html).toContain('aria-current="true"');
  });

  it("shows the error with a retry", () => {
    const html = renderToString(
      <PendingOrdersList {...base} errorMessage="Không kết nối được máy chủ." />,
    );
    expect(html).toContain("Không kết nối được máy chủ.");
    expect(html).toContain("Thử lại");
  });
});

describe("PendingOrdersButton", () => {
  it("shows the count and the ready badge", () => {
    const html = renderToString(<PendingOrdersButton count={3} readyCount={2} onClick={() => {}} />);
    expect(html).toContain("Đơn đang chờ (3)");
    expect(html).toContain("2 đơn sẵn sàng hoàn tất");
    expect(html).toContain("F4");
  });

  it("hides the ready badge at zero", () => {
    const html = renderToString(<PendingOrdersButton count={1} readyCount={0} onClick={() => {}} />);
    expect(html).not.toContain("sẵn sàng hoàn tất");
  });
});
