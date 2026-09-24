import { describe, expect, it } from "bun:test";
import {
  buildBoard,
  distinctCategories,
  minutesSince,
  formatElapsed,
  summarizeBulkOutcomes,
  NEXT_STATE,
  CORRECT_TARGET,
} from "./board";
import type { PreparationQueueUnitResponse, PreparationBulkAdvanceOutcome } from "@/api/generated/models";

function u(overrides: Partial<PreparationQueueUnitResponse>): PreparationQueueUnitResponse {
  return {
    id: "u1",
    item_name: "Cà phê sữa đá",
    state: "QUEUED",
    service_number: "001",
    table_names: [],
    category_name: "Đồ uống",
    queued_at: "2026-09-26T01:00:00Z",
    unit_number: 1,
    priority: "STANDARD",
    modifiers: [],
    ...overrides,
  };
}

describe("buildBoard", () => {
  it("groups units into their state column", () => {
    const board = buildBoard(
      [
        u({ id: "a", state: "QUEUED" }),
        u({ id: "b", state: "IN_PREPARATION" }),
        u({ id: "c", state: "READY" }),
      ],
      null,
    );
    expect(board.QUEUED).toHaveLength(1);
    expect(board.IN_PREPARATION).toHaveLength(1);
    expect(board.READY).toHaveLength(1);
  });

  it("groups units sharing a service_number into one ticket, within a column", () => {
    const board = buildBoard(
      [u({ id: "a", service_number: "010" }), u({ id: "b", service_number: "010" })],
      null,
    );
    expect(board.QUEUED).toHaveLength(1);
    expect(board.QUEUED[0].units.map((x) => x.id)).toEqual(["a", "b"]);
  });

  it("keeps the same service_number in separate tickets across columns", () => {
    const board = buildBoard(
      [
        u({ id: "a", service_number: "010", state: "QUEUED" }),
        u({ id: "b", service_number: "010", state: "IN_PREPARATION" }),
      ],
      null,
    );
    expect(board.QUEUED[0].units.map((x) => x.id)).toEqual(["a"]);
    expect(board.IN_PREPARATION[0].units.map((x) => x.id)).toEqual(["b"]);
  });

  it("ignores units in a terminal or exceptional state", () => {
    const board = buildBoard(
      [u({ id: "a", state: "FULFILLED" }), u({ id: "b", state: "WASTED" }), u({ id: "c", state: "CANCELLED" })],
      null,
    );
    expect(board.QUEUED).toHaveLength(0);
    expect(board.IN_PREPARATION).toHaveLength(0);
    expect(board.READY).toHaveLength(0);
  });

  it("filters by category when given one", () => {
    const board = buildBoard(
      [u({ id: "a", category_name: "Bar" }), u({ id: "b", category_name: "Bánh" })],
      "Bar",
    );
    expect(board.QUEUED.flatMap((t) => t.units.map((x) => x.id))).toEqual(["a"]);
  });

  it("flags a REMAKE-priority unit and ignores STANDARD", () => {
    const board = buildBoard(
      [u({ id: "a", priority: "REMAKE" }), u({ id: "b", priority: "STANDARD" })],
      null,
    );
    const byId = Object.fromEntries(board.QUEUED[0].units.map((x) => [x.id, x]));
    expect(byId.a.isRemake).toBe(true);
    expect(byId.b.isRemake).toBe(false);
  });

  it("joins modifier option names for display", () => {
    const board = buildBoard(
      [u({ id: "a", modifiers: [{ group_name: "Đá", option_name: "Ít đá" }, { group_name: "Đường", option_name: "50%" }] })],
      null,
    );
    expect(board.QUEUED[0].units[0].modifierSummary).toBe("Ít đá, 50%");
  });

  it("tolerates a missing list", () => {
    expect(buildBoard(undefined, null)).toEqual({ QUEUED: [], IN_PREPARATION: [], READY: [] });
  });
});

describe("distinctCategories", () => {
  it("lists each category once, alphabetically", () => {
    expect(distinctCategories([u({ category_name: "Trà" }), u({ category_name: "Bar" }), u({ category_name: "Bar" })])).toEqual([
      "Bar",
      "Trà",
    ]);
  });

  it("excludes exceptional units that remain only for alerts", () => {
    expect(
      distinctCategories([
        u({ category_name: "Bar", state: "QUEUED" }),
        u({ category_name: "Bếp", state: "WASTED" }),
      ]),
    ).toEqual(["Bar"]);
  });

  it("tolerates a missing list", () => {
    expect(distinctCategories(undefined)).toEqual([]);
  });
});

describe("elapsed time", () => {
  it("counts whole minutes and never goes negative", () => {
    const now = Date.parse("2026-09-26T01:10:30Z");
    expect(minutesSince("2026-09-26T01:00:00Z", now)).toBe(10);
    expect(minutesSince("2026-09-26T02:00:00Z", now)).toBe(0);
    expect(minutesSince(null, now)).toBe(0);
    expect(minutesSince("garbage", now)).toBe(0);
  });

  it("words the elapsed time", () => {
    expect(formatElapsed(0)).toBe("Vừa xong");
    expect(formatElapsed(7)).toBe("7 phút");
  });
});

describe("state transition tables", () => {
  it("advances one step forward per column", () => {
    expect(NEXT_STATE.QUEUED).toBe("IN_PREPARATION");
    expect(NEXT_STATE.IN_PREPARATION).toBe("READY");
    expect(NEXT_STATE.READY).toBe("FULFILLED");
  });

  it("corrects one step backward, except from Queued", () => {
    expect(CORRECT_TARGET.QUEUED).toBeNull();
    expect(CORRECT_TARGET.IN_PREPARATION).toBe("QUEUED");
    expect(CORRECT_TARGET.READY).toBe("IN_PREPARATION");
  });
});

describe("summarizeBulkOutcomes", () => {
  it("counts advanced and failed outcomes", () => {
    const outcomes: PreparationBulkAdvanceOutcome[] = [
      { preparation_unit_id: "a", status: "ADVANCED" },
      { preparation_unit_id: "b", status: "FAILED", code: "INVALID_TRANSITION" },
      { preparation_unit_id: "c", status: "ADVANCED" },
    ];
    expect(summarizeBulkOutcomes(outcomes)).toEqual({ succeeded: 2, failed: 1 });
  });

  it("tolerates an empty list", () => {
    expect(summarizeBulkOutcomes([])).toEqual({ succeeded: 0, failed: 0 });
  });
});
