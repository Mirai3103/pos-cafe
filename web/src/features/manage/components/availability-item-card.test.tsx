import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { StockCard } from "./availability-item-card";
import type { StockCard as StockCardData } from "../lib/availability-cards";

const base: StockCardData = {
  kind: "item",
  id: "i",
  name: "Cà phê đen",
  code: "CFD",
  groupName: "Cà phê Việt",
  scope: "c",
  detail: "S / L",
  available: true,
  sizes: [
    { id: "s", name: "Size S", available: true },
    { id: "l", name: "Size L", available: false },
  ],
  blockedBy: [],
  icon: "coffee",
  priceVnd: 25_000,
  imageUrl: null,
};

const render = (card: StockCardData) => renderToString(<StockCard card={card} onToggle={() => {}} />);

describe("StockCard", () => {
  it("shows name, code, price, group with sizes, state, and one switch per size", () => {
    const html = render(base);
    expect(html).toContain("Cà phê đen");
    expect(html).toContain("CFD");
    expect(html).toMatch(/25\.000/);
    expect(html).toContain("Cà phê Việt (S / L)");
    expect(html).toContain("Còn hàng");
    expect(html).toContain(">ON<");
    expect(html).toContain("Size S");
    expect(html).toContain("Size L");
    expect(html.match(/role="switch"/g)).toHaveLength(3);
    expect(html).toContain('aria-checked="false"');
  });

  it("marks a card that is off", () => {
    const html = render({ ...base, available: false });
    expect(html).toContain("Tạm hết");
    expect(html).toContain(">OFF<");
    expect(html).toContain("border-rose-300");
  });

  it("shows a dash when no price is known", () => {
    expect(render({ ...base, priceVnd: null })).toContain("—");
  });

  it("warns when a required group has run out", () => {
    expect(render({ ...base, blockedBy: ["Mức đường"] })).toContain("Không bán được: hết tùy chọn bắt buộc (Mức đường)");
  });

  it("renders a topping without size switches", () => {
    const html = render({ ...base, kind: "modifier_option", sizes: [], code: "TOP-TCT", detail: "1 phần thêm", groupName: "Topping thêm" });
    expect(html).toContain("Topping thêm (1 phần thêm)");
    expect(html.match(/role="switch"/g)).toHaveLength(1);
  });
});
