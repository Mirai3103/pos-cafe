import { describe, expect, it } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToString } from "react-dom/server";
import { CorrectStateDialog, type CorrectStateDialogProps } from "./correct-state-dialog";

const unit = {
  id: "u1",
  itemName: "Bạc xỉu đá",
  sizeName: null,
  modifierSummary: null,
  preparationNote: null,
  unitNumber: 2,
  state: "IN_PREPARATION" as const,
  queuedAt: "2026-09-26T01:00:00Z",
  inPreparationAt: "2026-09-26T01:02:00Z",
  categoryName: "Đồ uống",
  isRemake: false,
};

function renderDialog(props: CorrectStateDialogProps) {
  return renderToString(
    <QueryClientProvider client={new QueryClient()}>
      <CorrectStateDialog {...props} />
    </QueryClientProvider>,
  );
}

describe("CorrectStateDialog", () => {
  it("renders nothing without a target unit", () => {
    expect(
      renderToString(<CorrectStateDialog unit={null} column={null} onClose={() => {}} />),
    ).toBe("");
  });

  it("renders nothing for Queued, which has no correction target", () => {
    expect(
      renderToString(<CorrectStateDialog unit={unit} column="QUEUED" onClose={() => {}} />),
    ).toBe("");
  });

  it("shows the unit and both reason options", () => {
    const html = renderDialog({ unit, column: "IN_PREPARATION", onClose: () => {} });
    expect(html).toContain("Bạc xỉu đá");
    expect(html).toContain("Ghi nhận nhầm thao tác");
    expect(html).toContain("Khác");
    expect(html).toContain("Quản lý");
  });

  it("caps the note input at the backend's 500-rune limit", () => {
    const html = renderDialog({ unit, column: "IN_PREPARATION", onClose: () => {} });
    expect(html).toContain(`maxLength="500"`);
  });
});
