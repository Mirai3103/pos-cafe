import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { CategoryFilterBar } from "./category-filter-bar";

describe("CategoryFilterBar", () => {
  it("renders nothing when there are no categories", () => {
    expect(renderToString(<CategoryFilterBar categories={[]} active={null} onSelect={() => {}} />)).toBe("");
  });

  it("shows 'Tất cả' plus every category", () => {
    const html = renderToString(
      <CategoryFilterBar categories={["Bar", "Trà"]} active={null} onSelect={() => {}} />,
    );
    expect(html).toContain("Tất cả");
    expect(html).toContain("Bar");
    expect(html).toContain("Trà");
  });
});
