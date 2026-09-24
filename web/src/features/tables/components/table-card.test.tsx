import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TableCard } from "./table-card";
import type { FloorTable } from "../lib/floor";

const noop = () => {};

function render(table: FloorTable, canAdminister = false) {
  return renderToString(
    <TableCard
      table={table}
      canAdminister={canAdminister}
      onOpen={noop}
      onRename={noop}
      onToggleAvailability={noop}
    />,
  );
}

const base: FloorTable = { id: "t1", name: "Bàn 1", available: true, occupants: [], state: "FREE" };

describe("TableCard", () => {
  it("shows a free Table", () => {
    const html = render(base);
    expect(html).toContain("Bàn 1");
    expect(html).toContain("Trống");
  });

  it("lists every Service Number on an occupied Table", () => {
    const html = render({
      ...base,
      state: "OCCUPIED",
      occupants: [
        { sessionId: "s1", serviceNumber: "012" },
        { sessionId: "s2", serviceNumber: "015" },
      ],
    });
    expect(html).toContain("Có khách");
    expect(html).toContain("#012");
    expect(html).toContain("#015");
  });

  it("marks an unavailable Table and disables it", () => {
    const html = render({ ...base, available: false, state: "UNAVAILABLE" });
    expect(html).toContain("Tạm ngưng");
    expect(html).toContain("disabled");
  });

  it("marks an occupied Table that has been switched off", () => {
    const html = render({
      ...base,
      available: false,
      state: "OCCUPIED",
      occupants: [{ sessionId: "s1", serviceNumber: "012" }],
    });
    expect(html).toContain("Có khách");
    expect(html).toContain("Tạm ngưng");
  });

  it("offers admin actions only to administrators", () => {
    expect(render(base)).not.toContain("Đổi tên");
    expect(render(base, true)).toContain("Đổi tên");
    expect(render(base, true)).toContain("Tạm ngưng");
    expect(render({ ...base, available: false, state: "UNAVAILABLE" }, true)).toContain("Mở lại");
  });
});
