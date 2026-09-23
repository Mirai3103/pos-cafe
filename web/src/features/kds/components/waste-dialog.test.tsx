import { describe, expect, it } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToString } from "react-dom/server";
import { WasteDialog, type WasteDialogProps } from "./waste-dialog";

function renderDialog(props: WasteDialogProps) {
  return renderToString(
    <QueryClientProvider client={new QueryClient()}>
      <WasteDialog {...props} />
    </QueryClientProvider>,
  );
}

describe("WasteDialog", () => {
  it("renders nothing without a target unit", () => {
    expect(renderDialog({ unit: null, onClose: () => {} })).toBe("");
  });

  it("shows the unit and every reason option", () => {
    const html = renderDialog({
      unit: {
        id: "u1",
        itemName: "Bạc xỉu đá",
        sizeName: null,
        modifierSummary: null,
        preparationNote: null,
        unitNumber: 2,
        state: "IN_PREPARATION",
        queuedAt: "2026-09-26T01:00:00Z",
        inPreparationAt: "2026-09-26T01:02:00Z",
        categoryName: "Đồ uống",
        isRemake: false,
      },
      onClose: () => {},
    });
    expect(html).toContain("Bạc xỉu đá");
    expect(html).toContain("Lỗi pha chế");
    expect(html).toContain("Không đạt chất lượng");
    expect(html).toContain("Khách yêu cầu");
    expect(html).toContain("Khác");
  });
});
