// web/src/features/catalog/components/group-form-panel.test.tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { GroupFormPanel, type GroupFormPanelProps } from "./group-form-panel";

function props(patch: Partial<GroupFormPanelProps>): GroupFormPanelProps {
  return {
    form: {
      name: "Mức đá",
      min: 1,
      max: 1,
      rows: [{ key: "a", id: "o-a", name: "100% đá", surchargeVnd: 0, isDefault: true }],
      retiredOptions: [],
    },
    errors: {},
    isEdit: true,
    busy: false,
    results: null,
    focusKey: null,
    onChange: () => {},
    onAddRow: () => {},
    onRemoveRow: () => {},
    onSave: () => {},
    onClose: () => {},
    ...patch,
  };
}

describe("GroupFormPanel", () => {
  it("renders the rule choice, rows, and footer", () => {
    const html = renderToString(<GroupFormPanel {...props({ onRetire: () => {} })} />);
    expect(html).toContain("Chọn 1 duy nhất (Radio)");
    expect(html).toContain("Chọn nhiều (Checkbox)");
    expect(html).toContain("100% đá");
    expect(html).toContain("+ Thêm dòng Topping");
    expect(html).toContain("Xóa nhóm topping");
    expect(html).toContain("LƯU NHÓM TOPPING");
  });

  it("shows rule errors", () => {
    const html = renderToString(
      <GroupFormPanel {...props({ errors: { defaults: "Số lựa chọn mặc định phải từ tối thiểu đến tối đa" } })} />,
    );
    expect(html).toContain("Số lựa chọn mặc định phải từ tối thiểu đến tối đa");
  });
});
