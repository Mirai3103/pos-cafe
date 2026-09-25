import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { AvailabilityStats } from "./availability-stats";
import { ItemCountBadge, TabCounter } from "./availability-counter";

const summary = { items: 12, toppings: 4, total: 16, available: 14, unavailable: 2 };

describe("AvailabilityStats", () => {
  it("shows the design's four cards with the restore count", () => {
    const html = renderToString(<AvailabilityStats summary={summary} restoreCount={3} onRestore={() => {}} />);
    expect(html).toContain("Tổng danh mục");
    expect(html).toContain("12 món + 4 topping");
    expect(html).toContain("Đang còn hàng");
    expect(html).toContain("Bình thường tại quầy");
    expect(html).toContain("Tạm hết hàng");
    expect(html).toContain("Khóa chọn trên POS");
    expect(html).toContain("Thao tác kho nhanh");
    expect(html).toContain("Khôi phục tất cả Còn hàng (3)");
    expect(html).not.toMatch(/<button[^>]*\sdisabled=""/);
  });

  it("disables restore when nothing is off", () => {
    const html = renderToString(<AvailabilityStats summary={{ ...summary, available: 16, unavailable: 0 }} restoreCount={0} onRestore={() => {}} />);
    expect(html).toMatch(/<button[^>]*\sdisabled=""[^>]*>[\s\S]*Khôi phục tất cả Còn hàng</);
    expect(html).not.toContain("Còn hàng (0)");
  });
});

describe("tab badges", () => {
  it("shows the unavailable count in rose, and zero in emerald", () => {
    const two = renderToString(<TabCounter count={2} />);
    expect(two).toContain("2 tạm hết");
    expect(two).toContain("bg-rose-600");
    const zero = renderToString(<TabCounter count={0} />);
    expect(zero).toContain("0 tạm hết");
    expect(zero).toContain("bg-emerald-600/80");
  });

  it("shows the item count on the catalog tab", () => {
    expect(renderToString(<ItemCountBadge count={11} />)).toContain("11 món");
  });
});
