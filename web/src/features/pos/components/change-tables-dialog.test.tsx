import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { ChangeTablesChoices, ChangeTablesDialog } from "./change-tables-dialog";
import type { FloorTable } from "@/features/tables/lib/floor";

const tables: FloorTable[] = [
  { id: "t1", name: "Bàn 1", available: true, occupants: [{ sessionId: "s1", serviceNumber: "012" }], state: "OCCUPIED" },
  { id: "t2", name: "Bàn 2", available: true, occupants: [], state: "FREE" },
  { id: "t3", name: "Bàn 3", available: true, occupants: [{ sessionId: "s9", serviceNumber: "020" }], state: "OCCUPIED" },
  { id: "t4", name: "Bàn 4", available: false, occupants: [], state: "UNAVAILABLE" },
];

describe("ChangeTablesDialog", () => {
  it("renders nothing without a Session", () => {
    expect(renderToString(<ChangeTablesDialog session={null} onClose={() => {}} />)).toBe("");
  });
});

describe("ChangeTablesChoices", () => {
  it("lists held and available Tables, marks other parties, omits unavailable ones", () => {
    const html = renderToString(
      <ChangeTablesChoices tables={tables} sessionId="s1" currentIds={["t1"]} selected={["t1"]} onToggle={() => {}} />,
    );
    expect(html).toContain("Bàn 1");
    expect(html).not.toContain("#012");
    expect(html).toContain("Bàn 2");
    expect(html).toContain("Bàn 3");
    expect(html).toContain("#020");
    expect(html).not.toContain("Bàn 4");
  });
});
