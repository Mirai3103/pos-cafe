import { describe, expect, it } from "bun:test";
import {
  TABLES_OVERVIEW_POLL_MS,
  useSetSessionTables,
  useStartDineInSession,
  useTableAdmin,
  useTablesOverview,
} from "./use-tables";

describe("tables API seam", () => {
  it("polls the floor every ten seconds", () => {
    expect(TABLES_OVERVIEW_POLL_MS).toBe(10_000);
  });

  it("exports every hook the floor and the POS use", () => {
    for (const hook of [useTablesOverview, useStartDineInSession, useSetSessionTables, useTableAdmin]) {
      expect(typeof hook).toBe("function");
    }
  });
});
