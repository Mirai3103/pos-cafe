import { describe, expect, it } from "bun:test";
import { floorStats, tableCardState, toFloorTables, toggleSelection } from "./floor";

const occupant = { service_session_id: "s1", service_number: "012" };

describe("tableCardState", () => {
  it("is FREE when available and unoccupied", () => {
    expect(tableCardState({ id: "t", available: true, current_service_sessions: [] })).toBe("FREE");
  });

  it("is OCCUPIED whenever a Session sits there, available or not", () => {
    expect(tableCardState({ id: "t", available: true, current_service_sessions: [occupant] })).toBe("OCCUPIED");
    expect(tableCardState({ id: "t", available: false, current_service_sessions: [occupant] })).toBe("OCCUPIED");
  });

  it("is UNAVAILABLE when switched off and empty", () => {
    expect(tableCardState({ id: "t", available: false })).toBe("UNAVAILABLE");
  });
});

describe("toFloorTables", () => {
  it("maps rows, drops rows without an id, keeps server order", () => {
    const tables = toFloorTables([
      { id: "t2", name: "Bàn 2", available: true, current_service_sessions: [occupant] },
      { name: "no id" },
      { id: "t1", name: "Bàn 1", available: true },
    ]);
    expect(tables.map((t) => t.id)).toEqual(["t2", "t1"]);
    expect(tables[0]).toEqual({
      id: "t2",
      name: "Bàn 2",
      available: true,
      occupants: [{ sessionId: "s1", serviceNumber: "012" }],
      state: "OCCUPIED",
    });
  });

  it("answers an empty list for no data", () => {
    expect(toFloorTables(undefined)).toEqual([]);
  });
});

describe("floorStats", () => {
  it("counts each state", () => {
    const tables = toFloorTables([
      { id: "a", available: true },
      { id: "b", available: true, current_service_sessions: [occupant] },
      { id: "c", available: false },
    ]);
    expect(floorStats(tables)).toEqual({ total: 3, free: 1, occupied: 1, unavailable: 1 });
  });
});

describe("toggleSelection", () => {
  it("adds a missing id and removes a present one", () => {
    expect(toggleSelection(["a"], "b")).toEqual(["a", "b"]);
    expect(toggleSelection(["a", "b"], "a")).toEqual(["b"]);
  });
});
