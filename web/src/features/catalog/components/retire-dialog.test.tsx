// web/src/features/catalog/components/retire-dialog.test.tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { RETIRE_WARNING, RetirePanel } from "./retire-dialog";

const base = {
  name: "Trà đào",
  value: { reason: "NO_LONGER_OFFERED" as const, note: "" },
  error: null,
  busy: false,
  onChange: () => {},
  onConfirm: () => {},
  onClose: () => {},
};

describe("RetirePanel", () => {
  it("names the entity, warns, and offers the three reasons", () => {
    const html = renderToString(<RetirePanel {...base} />);
    expect(html).toContain("Trà đào");
    expect(html).toContain(RETIRE_WARNING);
    for (const label of ["Không bán nữa", "Sắp xếp lại thực đơn", "Khác"]) expect(html).toContain(label);
  });

  it("shows an inline error", () => {
    expect(renderToString(<RetirePanel {...base} error="Vui lòng ghi chú lý do" />)).toContain("Vui lòng ghi chú lý do");
  });
});
