import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { CatalogHeader, CatalogPills } from "./catalog-nav";

describe("catalog nav", () => {
  it("renders the four pills with counts and marks the active one", () => {
    const html = renderToString(
      <CatalogPills view="groups" counts={{ items: 12, categories: 5, groups: 4 }} onChange={() => {}} />,
    );
    // renderToString escapes "&" as "&amp;".
    for (const label of ["Món &amp; Định giá", "Danh mục món", "Nhóm Topping", "Ma trận Gán Topping (Batch Linker)"]) {
      expect(html).toContain(label);
    }
    expect(html).toContain("12 món");
    expect(html).toContain("5 danh mục");
    expect(html).toContain("4 nhóm");
    expect(html).toMatch(/aria-selected="true"[^>]*>[\s\S]*?Nhóm Topping/);
  });

  it("renders the hero with the add button", () => {
    const html = renderToString(<CatalogHeader canAdd onNewItem={() => {}} />);
    expect(html).toContain("Trung tâm Quản lý Thực đơn &amp; Nhóm Topping");
    expect(html).toContain("+ THÊM MÓN MỚI (Ctrl+N)");
  });
});
