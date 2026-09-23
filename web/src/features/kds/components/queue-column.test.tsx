import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { QueueColumn } from "./queue-column";
import type { BoardTicket } from "../lib/board";

const base = {
  column: "QUEUED" as const,
  nowMs: Date.parse("2026-09-26T01:03:00Z"),
  busy: false,
  failedUnitIds: new Set<string>(),
  onAdvance: () => {},
  onRequestWaste: () => {},
  onRequestCorrectState: () => {},
};

const ticket: BoardTicket = {
  serviceNumber: "014",
  tableNames: [],
  units: [
    {
      id: "u1",
      itemName: "Bạc xỉu đá",
      sizeName: null,
      modifierSummary: null,
      preparationNote: null,
      unitNumber: 1,
      state: "QUEUED",
      queuedAt: "2026-09-26T01:00:00Z",
      inPreparationAt: null,
      categoryName: "Đồ uống",
      isRemake: false,
    },
  ],
};

describe("QueueColumn", () => {
  it("shows the column title and unit count", () => {
    const html = renderToString(<QueueColumn {...base} tickets={[ticket]} />);
    expect(html).toContain("Chờ pha");
    expect(html).toContain("014");
  });

  it("says so when the column is empty", () => {
    expect(renderToString(<QueueColumn {...base} tickets={[]} />)).toContain("Không có đơn");
  });
});
