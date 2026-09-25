import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { AvailabilityItemCard } from "./availability-item-card";
import type { AvailabilityItemView } from "../lib/availability";

const base: AvailabilityItemView = {
  id: "i",
  name: "Cà phê đen",
  categoryId: "c",
  categoryName: "Cà phê",
  available: true,
  sizes: [
    { id: "s", name: "Size S", available: true },
    { id: "l", name: "Size L", available: false },
  ],
  blockedBy: [],
};

describe("AvailabilityItemCard", () => {
  it("shows name, category, state, and one chip per size", () => {
    const html = renderToString(<AvailabilityItemCard item={base} onToggle={() => {}} />);
    expect(html).toContain("Cà phê đen");
    expect(html).toContain("Cà phê");
    expect(html).toContain("Còn hàng");
    expect(html).toContain("Size S");
    expect(html).toContain("Size L");
    expect(html).toContain('aria-checked="false"');
  });

  it("marks an item that is off", () => {
    const html = renderToString(<AvailabilityItemCard item={{ ...base, available: false }} onToggle={() => {}} />);
    expect(html).toContain("Tạm hết");
  });

  it("warns when a required group has run out", () => {
    const html = renderToString(<AvailabilityItemCard item={{ ...base, blockedBy: ["Mức đường"] }} onToggle={() => {}} />);
    expect(html).toContain("Không bán được: hết tùy chọn bắt buộc (Mức đường)");
  });
});
