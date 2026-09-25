import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { AvailabilityStats } from "./availability-stats";
import { TabCounter } from "./availability-counter";

describe("AvailabilityStats", () => {
  it("shows the counts and the restore count", () => {
    const html = renderToString(<AvailabilityStats stats={{ total: 8, available: 5, unavailable: 3 }} onRestore={() => {}} />);
    expect(html).toContain("Tổng danh mục");
    expect(html).toContain("Đang còn hàng");
    expect(html).toContain("Tạm hết hàng");
    expect(html).toContain("Khôi phục tất cả còn hàng (3)");
    expect(html).not.toMatch(/<button[^>]*\sdisabled=""/);
  });

  it("disables restore when nothing is off", () => {
    const html = renderToString(<AvailabilityStats stats={{ total: 8, available: 8, unavailable: 0 }} onRestore={() => {}} />);
    expect(html).toMatch(/<button[^>]*\sdisabled=""[^>]*>[\s\S]*Khôi phục tất cả còn hàng \(0\)/);
  });
});

describe("TabCounter", () => {
  it("renders the count, and nothing at zero", () => {
    expect(renderToString(<TabCounter count={2} />)).toContain("2 tạm hết");
    expect(renderToString(<TabCounter count={0} />)).toBe("");
  });
});
