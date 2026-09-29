// web/src/features/catalog/components/item-form-panel.test.tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import type { CatCategory, CatGroup } from "../lib/catalog-model";
import { itemFormFrom } from "../lib/forms";
import { ItemFormPanel, type ItemFormPanelProps } from "./item-form-panel";

const categories: CatCategory[] = [{ id: "c-tea", name: "Trà", icon: null, displayOrder: 1, groupIds: ["g-ice"] }];
const groups: CatGroup[] = [
  { id: "g-ice", name: "Mức đá", min: 1, max: 1, options: [], defaultOptionIds: [] },
  { id: "g-top", name: "Topping trà", min: 0, max: 3, options: [], defaultOptionIds: [] },
];

function props(patch: Partial<ItemFormPanelProps>): ItemFormPanelProps {
  return {
    form: itemFormFrom(null, "c-tea"),
    errors: {},
    isEdit: false,
    busy: false,
    results: null,
    categories,
    groups,
    storedImageUrl: null,
    onChange: () => {},
    onPickImage: () => {},
    onRemoveSize: () => {},
    onSave: () => {},
    onClose: () => {},
    ...patch,
  };
}

describe("ItemFormPanel", () => {
  it("offers the pricing mode toggle only when creating", () => {
    expect(renderToString(<ItemFormPanel {...props({})} />)).toContain("Định giá Đa Kích cỡ");
    expect(renderToString(<ItemFormPanel {...props({ isEdit: true })} />)).not.toContain("Định giá Đa Kích cỡ");
  });

  it("lists inherited groups apart from the extra group chips", () => {
    const html = renderToString(<ItemFormPanel {...props({})} />);
    expect(html).toContain("Nhóm tùy chọn Kế thừa từ Danh mục");
    expect(html).toContain("Mức đá");
    expect(html).toContain("Topping trà");
  });

  it("shows field errors and the retire button when editing", () => {
    const html = renderToString(
      <ItemFormPanel {...props({ isEdit: true, errors: { name: "Vui lòng nhập tên món" }, onRetire: () => {} })} />,
    );
    expect(html).toContain("Vui lòng nhập tên món");
    expect(html).toContain("Xóa món ăn");
    expect(html).toContain("LƯU MÓN ĂN (Ctrl+S)");
  });
});
