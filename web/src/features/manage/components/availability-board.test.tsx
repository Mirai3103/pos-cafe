import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { AvailabilityBoard } from "./availability-board";
import { EMPTY_FILTER, TOPPINGS_SCOPE, toAvailabilityView } from "../lib/availability";

const view = toAvailabilityView({
  categories: [
    {
      id: "c",
      name: "Cà phê",
      items: [
        {
          id: "i",
          name: "Cà phê đen",
          available: true,
          modifier_groups: [
            { id: "g", name: "Topping thêm", min_selections: 0, options: [{ id: "o", name: "Trân châu trắng", available: false }] },
          ],
        },
      ],
    },
  ],
});

const render = (filter = EMPTY_FILTER, v = view) =>
  renderToString(<AvailabilityBoard view={v} filter={filter} onFilterChange={() => {}} onToggle={() => {}} onRestore={() => {}} />);

describe("AvailabilityBoard", () => {
  it("renders category pills, the topping pill, items, and toppings", () => {
    const html = render();
    expect(html).toContain("Tất cả");
    expect(html).toContain("Topping");
    expect(html).toContain("Cà phê đen");
    expect(html).toContain("Trân châu trắng");
    expect(html).toContain("Chỉ xem món Tạm hết");
    expect(html).toContain("Tìm theo tên món hoặc mã (cfsd, bạc xỉu, trà đào, topping...)");
  });

  it("shows the active only-unavailable filter the design's way", () => {
    const html = render({ ...EMPTY_FILTER, onlyUnavailable: true });
    expect(html).toContain("Đang lọc: Chỉ món Tạm hết");
    expect(html).not.toContain("Cà phê đen");
    expect(html).toContain("Trân châu trắng");
  });

  it("shows only toppings under the topping pill", () => {
    const html = render({ ...EMPTY_FILTER, scope: TOPPINGS_SCOPE });
    expect(html).not.toContain("Cà phê đen");
    expect(html).toContain("Trân châu trắng");
  });

  it("shows the no-match state with a reset", () => {
    const html = render({ ...EMPTY_FILTER, query: "khong co mon nay" });
    expect(html).toContain("Không tìm thấy món hoặc topping phù hợp");
    expect(html).toContain("Xóa bộ lọc &amp; Hiển thị tất cả");
  });

  it("shows the empty-menu state", () => {
    expect(render(EMPTY_FILTER, toAvailabilityView(null))).toContain("Chưa có món nào trong thực đơn");
  });
});
