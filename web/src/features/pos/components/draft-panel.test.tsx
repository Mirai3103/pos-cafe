import { describe, it, expect, mock } from "bun:test";
import { renderToString } from "react-dom/server";

// Mock @tanstack/react-router to allow Link rendering in SSR tests
mock.module("@tanstack/react-router", () => ({
  Link: ({ children, to, className }: { children?: React.ReactNode; to?: string; className?: string }) => (
    <a href={to} className={className}>
      {children}
    </a>
  ),
}));

import { NoShiftNotice } from "./no-shift-notice";
import { DraftEmptyState } from "./draft-empty-state";
import { DraftItemRow } from "./draft-item-row";
import { DraftPanel } from "./draft-panel";
import type { SalesDraftItemResponse, SalesServiceSessionResponse } from "@/api/generated/models";
import { formatVND } from "@/lib/utils";

const mockItem1: SalesDraftItemResponse = {
  id: "draft-item-1",
  menu_item_id: "item-cf-1",
  name: "Cà phê sữa đá",
  size_id: "size-m",
  size_name: "Vừa (M)",
  price_vnd: 30000,
  quantity: 2,
  available: true,
  preparation_note: "Ít sữa",
  selected_modifier_options: [
    { id: "opt-sugar-50", name: "50% đường", surcharge_vnd: 0 },
    { id: "opt-flan", name: "Bánh Flan", surcharge_vnd: 8000 },
  ],
};

const mockItemUnavailable: SalesDraftItemResponse = {
  id: "draft-item-2",
  menu_item_id: "item-tea-1",
  name: "Trà đào cam sả",
  price_vnd: 35000,
  quantity: 1,
  available: false,
};

const mockSessionWithItems: SalesServiceSessionResponse = {
  id: "session-1",
  service_number: "042",
  service_mode: "takeaway",
  draft: {
    items: [mockItem1, mockItemUnavailable],
  },
};

const mockEmptySession: SalesServiceSessionResponse = {
  id: "session-2",
  service_number: "043",
  service_mode: "takeaway",
  draft: {
    items: [],
  },
};

describe("NoShiftNotice", () => {
  it("exports NoShiftNotice component function", () => {
    expect(typeof NoShiftNotice).toBe("function");
  });

  it("renders notice with icon, heading, description, and link to /shift", () => {
    const html = renderToString(<NoShiftNotice />);

    expect(html).toContain("Chưa có ca bán hàng mở");
    expect(html).toContain("Bạn vẫn có thể xem thực đơn, nhưng cần mở ca để bắt đầu tạo đơn và tính tiền.");
    expect(html).toContain('href="/shift"');
    expect(html).toContain("Mở ca làm việc");
    expect(html).toContain("min-h-[48px]");
  });
});

describe("DraftEmptyState", () => {
  it("exports DraftEmptyState component function", () => {
    expect(typeof DraftEmptyState).toBe("function");
  });

  it("renders shopping bag icon and Vietnamese empty state messages", () => {
    const html = renderToString(<DraftEmptyState />);

    expect(html).toContain("Chưa có món nào trong đơn");
    expect(html).toContain("Chạm vào món từ thực đơn bên trái để thêm vào đơn mang đi");
  });
});

describe("DraftItemRow", () => {
  it("exports DraftItemRow component function", () => {
    expect(typeof DraftItemRow).toBe("function");
  });

  it("renders item name, size, modifiers, preparation note, and calculates unit and line total", () => {
    const html = renderToString(
      <DraftItemRow
        item={mockItem1}
        onEdit={mock()}
        onQuantityChange={mock()}
        onRemove={mock()}
      />,
    );

    // Name and size
    expect(html).toContain("Cà phê sữa đá");
    expect(html).toContain("Vừa (M)");

    // Modifiers and note
    expect(html).toContain("50% đường, Bánh Flan");
    expect(html).toContain("Ít sữa");

    // Unit price: 30000 + 8000 = 38000 VND
    expect(html).toContain(formatVND(38000));

    // Quantity: 2
    expect(html).toContain(">2</span>");

    // Line total: 38000 * 2 = 76000 VND
    expect(html).toContain(formatVND(76000));

    // Monospace numbers
    expect(html).toContain("font-mono");
    expect(html).toContain("tabular-nums");

    // Delete button
    expect(html).toContain('aria-label="Xóa món"');
  });

  it("renders unavailable warning badge when available is false", () => {
    const html = renderToString(
      <DraftItemRow
        item={mockItemUnavailable}
        onEdit={mock()}
        onQuantityChange={mock()}
        onRemove={mock()}
      />,
    );

    expect(html).toContain("Trà đào cam sả");
    expect(html).toContain("Tạm hết hàng");
  });

  it("disables decrement button when quantity <= 1", () => {
    const html = renderToString(
      <DraftItemRow
        item={mockItemUnavailable} // quantity: 1
        onEdit={mock()}
        onQuantityChange={mock()}
        onRemove={mock()}
      />,
    );

    expect(html).toMatch(/<button[^>]*disabled[^>]*aria-label="Giảm số lượng"/);
  });

  it("disables increment button when quantity >= 9999", () => {
    const maxQtyItem: SalesDraftItemResponse = {
      ...mockItem1,
      quantity: 9999,
    };
    const html = renderToString(
      <DraftItemRow
        item={maxQtyItem}
        onEdit={mock()}
        onQuantityChange={mock()}
        onRemove={mock()}
      />,
    );

    expect(html).toMatch(/<button[^>]*disabled[^>]*aria-label="Tăng số lượng"/);
  });
});

describe("DraftPanel", () => {
  it("exports DraftPanel component function", () => {
    expect(typeof DraftPanel).toBe("function");
  });

  it("renders NoShiftNotice when isShiftOpen is false", () => {
    const html = renderToString(
      <DraftPanel
        session={mockSessionWithItems}
        isShiftOpen={false}
        onEditItem={mock()}
        onQuantityChange={mock()}
        onRemoveItem={mock()}
      />,
    );

    expect(html).toContain("Chưa có ca bán hàng mở");
    expect(html).toContain("Mở ca làm việc");
    expect(html).not.toContain("Cà phê sữa đá");
  });

  it("renders DraftEmptyState when cart is empty", () => {
    const html = renderToString(
      <DraftPanel
        session={mockEmptySession}
        isShiftOpen={true}
        onEditItem={mock()}
        onQuantityChange={mock()}
        onRemoveItem={mock()}
      />,
    );

    expect(html).toContain("Chưa có món nào trong đơn");
    expect(html).toContain("Chạm vào món từ thực đơn bên trái để thêm vào đơn mang đi");
  });

  it("renders DraftEmptyState when session is null and shift is open", () => {
    const html = renderToString(
      <DraftPanel
        session={null}
        isShiftOpen={true}
        onEditItem={mock()}
        onQuantityChange={mock()}
        onRemoveItem={mock()}
      />,
    );

    expect(html).toContain("Chưa có món nào trong đơn");
  });

  it("renders order items, totals, and thermal receipt aside layout", () => {
    const html = renderToString(
      <DraftPanel
        session={mockSessionWithItems}
        isShiftOpen={true}
        onEditItem={mock()}
        onQuantityChange={mock()}
        onRemoveItem={mock()}
      />,
    );

    // Thermal receipt aside layout
    expect(html).toContain("md:w-[380px]");
    expect(html).toContain("lg:w-[420px]");

    // Service number and takeaway badge
    expect(html).toContain("Đơn mang đi #042");
    expect(html).toContain("Mang đi");
    expect(html).toContain("Khách mua mang về");

    // Items rendered
    expect(html).toContain("Cà phê sữa đá");
    expect(html).toContain("Trà đào cam sả");

    // Subtotal: 76000 + 35000 = 111,000 VND
    expect(html).toContain("Tạm tính");
    expect(html).toContain("2");
    expect(html).toContain("món");
    expect(html).toContain(formatVND(111000));
    expect(html).toContain("Tổng cộng");

    // Disabled CTAs per scope boundaries
    expect(html).toContain("Hủy đơn");
    expect(html).toContain("Tại bàn (F2)");
    expect(html).toContain("Tiền mặt");
    expect(html).toContain("VietQR");
    expect(html).toContain("Thanh toán (F9)");
    expect(html).toContain("Mở ở Slice 4");
  });

  it("renders fallback header when service_number is omitted", () => {
    const sessionWithoutNumber: SalesServiceSessionResponse = {
      ...mockSessionWithItems,
      service_number: undefined,
    };
    const html = renderToString(
      <DraftPanel
        session={sessionWithoutNumber}
        isShiftOpen={true}
        onEditItem={mock()}
        onQuantityChange={mock()}
        onRemoveItem={mock()}
      />,
    );

    expect(html).toContain("Đơn mang đi");
    expect(html).not.toContain("Đơn mang đi #");
  });
});
