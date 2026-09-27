// web/src/features/catalog/components/category-form-panel.test.tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { CategoryFormPanel } from "./category-form-panel";

describe("CategoryFormPanel", () => {
  it("renders the fields, default-group chips, and footer", () => {
    const html = renderToString(
      <CategoryFormPanel
        form={{ name: "Trà", icon: "leaf", displayOrder: 2, groupIds: ["g-ice"] }}
        errors={{ displayOrder: "Thứ tự hiển thị từ 0 đến 9999" }}
        isEdit
        busy={false}
        results={null}
        groups={[{ id: "g-ice", name: "Mức đá", min: 1, max: 1, options: [], defaultOptionIds: [] }]}
        onChange={() => {}}
        onSave={() => {}}
        onClose={() => {}}
        onRetire={() => {}}
      />,
    );
    expect(html).toContain("Tên danh mục");
    expect(html).toContain("Biểu tượng danh mục (Icon)");
    expect(html).toContain("Mức đá");
    expect(html).toContain("Thứ tự hiển thị từ 0 đến 9999");
    expect(html).toContain("Xóa danh mục");
    expect(html).toContain("LƯU DANH MỤC");
  });
});
