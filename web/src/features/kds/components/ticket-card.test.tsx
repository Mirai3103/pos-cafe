import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { resolveUnitSelection, TicketCard } from "./ticket-card";
import type { BoardTicket } from "../lib/board";

function ticket(overrides: Partial<BoardTicket> = {}): BoardTicket {
  return {
    serviceNumber: "014",
    tableNames: [],
    units: [
      {
        id: "u1",
        itemName: "Bạc xỉu đá",
        sizeName: "L",
        modifierSummary: "Ít ngọt",
        preparationNote: null,
        unitNumber: 1,
        state: "QUEUED",
        queuedAt: "2026-09-26T01:00:00Z",
        inPreparationAt: null,
        categoryName: "Đồ uống",
        isRemake: false,
      },
    ],
    ...overrides,
  };
}

const base = {
  column: "QUEUED" as const,
  nowMs: Date.parse("2026-09-26T01:03:00Z"),
  busy: false,
  failedUnitIds: new Set<string>(),
  onAdvance: () => {},
  onRequestWaste: () => {},
  onRequestCorrectState: () => {},
};

const wasteTitle = "Huỷ món (lỗi pha chế, không đạt, khách yêu cầu...)";

describe("TicketCard", () => {
  it("shows the ticket's service number and its unit's item name", () => {
    const html = renderToString(<TicketCard {...base} ticket={ticket()} />);
    expect(html).toContain("014");
    expect(html).toContain("Bạc xỉu đá");
  });

  it("shows table names for a dine-in ticket", () => {
    const html = renderToString(<TicketCard {...base} ticket={ticket({ tableNames: ["T1-01"] })} />);
    expect(html).toContain("T1-01");
  });

  it("labels the primary action for its column", () => {
    expect(renderToString(<TicketCard {...base} ticket={ticket()} column="QUEUED" />)).toContain("Bắt đầu làm");
    expect(renderToString(<TicketCard {...base} ticket={ticket()} column="IN_PREPARATION" />)).toContain("Xong món");
    expect(renderToString(<TicketCard {...base} ticket={ticket()} column="READY" />)).toContain("Đã giao khách");
  });

  it("flags a Remake unit", () => {
    const remade = ticket({ units: [{ ...ticket().units[0], isRemake: true }] });
    expect(renderToString(<TicketCard {...base} ticket={remade} />)).toContain("PHA LẠI");
  });

  it("shows Waste only after preparation has started", () => {
    const queued = renderToString(<TicketCard {...base} ticket={ticket()} column="QUEUED" />);
    const inPrep = renderToString(<TicketCard {...base} ticket={ticket()} column="IN_PREPARATION" />);
    expect(queued).not.toContain(wasteTitle);
    expect(inPrep).toContain(wasteTitle);
  });

  it("shows the undo action only when the column has a correction target", () => {
    const queued = renderToString(<TicketCard {...base} ticket={ticket()} column="QUEUED" />);
    const inPrep = renderToString(<TicketCard {...base} ticket={ticket()} column="IN_PREPARATION" />);
    expect(queued).not.toContain("Hoàn tác");
    expect(inPrep).toContain("Hoàn tác");
  });

  it("shows the disabled shortage and reprint affordances", () => {
    const html = renderToString(<TicketCard {...base} ticket={ticket()} />);
    expect(html).toContain("Chưa hỗ trợ");
  });

  it("shows a bulk-advance failure on the affected unit line", () => {
    const html = renderToString(
      <TicketCard {...base} ticket={ticket()} failedUnitIds={new Set(["u1"])} />,
    );

    expect(html).toContain("Không thể chuyển trạng thái");
  });

  it("targets current units when every selected unit has left the ticket", () => {
    const replacement = { ...ticket().units[0], id: "u2" };

    expect(resolveUnitSelection(new Set(["u1"]), [replacement])).toEqual({
      selectedUnitIds: [],
      targetUnitIds: ["u2"],
    });
  });
});
