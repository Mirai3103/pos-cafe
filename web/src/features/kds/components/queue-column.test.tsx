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

const secondUnit = {
  ...ticket.units[0],
  id: "u2",
  unitNumber: 2,
};

describe("QueueColumn", () => {
  it("shows the column title and the aggregate unit count across tickets", () => {
    const html = renderToString(
      <QueueColumn {...base} tickets={[{ ...ticket, units: [ticket.units[0], secondUnit] }]} />,
    );
    expect(html).toContain("Chờ pha");
    expect(html).toContain("bg-amber-50 text-amber-800");
    expect(html).toContain(">2</span>");
  });

  it("says so when the column is empty", () => {
    expect(renderToString(<QueueColumn {...base} tickets={[]} />)).toContain("Không có đơn");
  });

  it("words the empty state differently while a category filter is active", () => {
    const html = renderToString(<QueueColumn {...base} tickets={[]} filterActive />);
    expect(html).toContain("Không có món trong nhóm này");
    expect(html).not.toContain("Không có đơn");
  });
});
