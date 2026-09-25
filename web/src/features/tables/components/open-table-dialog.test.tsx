import { describe, expect, it } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToString } from "react-dom/server";
import { OpenTableDialog } from "./open-table-dialog";
import type { FloorTable } from "../lib/floor";

const tables: FloorTable[] = [
  { id: "t1", name: "Bàn 1", available: true, occupants: [], state: "FREE" },
  { id: "t2", name: "Bàn 2", available: true, occupants: [], state: "FREE" },
  { id: "t3", name: "Bàn 3", available: false, occupants: [], state: "UNAVAILABLE" },
];

function render(initialTableId: string | null) {
  return renderToString(
    <QueryClientProvider client={new QueryClient()}>
      <OpenTableDialog initialTableId={initialTableId} tables={tables} onClose={() => {}} onOpened={() => {}} />
    </QueryClientProvider>,
  );
}

describe("OpenTableDialog", () => {
  it("renders nothing without a Table", () => {
    expect(render(null)).toBe("");
  });

  it("offers every available Table and omits unavailable ones", () => {
    const html = render("t1");
    expect(html).toContain("Bàn 1");
    expect(html).toContain("Bàn 2");
    expect(html).not.toContain("Bàn 3");
    expect(html).toContain("Mở bàn");
  });

  it("preselects the tapped Table", () => {
    const html = render("t1");
    expect(html.split('aria-pressed="true"').length - 1).toBe(1);
  });
});
