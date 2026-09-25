import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { FloorGrid } from "./tables-view";
import type { FloorTable } from "../lib/floor";

const tables: FloorTable[] = [
  { id: "t1", name: "Bàn 1", available: true, occupants: [], state: "FREE" },
];

describe("FloorGrid", () => {
  it("shows the empty state with the add action for administrators", () => {
    const html = renderToString(
      <FloorGrid tables={[]} canAdminister onOpen={() => {}} onRename={() => {}} onToggleAvailability={() => {}} onCreate={() => {}} />,
    );
    expect(html).toContain("Chưa có bàn nào");
    expect(html).toContain("Thêm bàn");
  });

  it("hides the add action from cashiers", () => {
    const html = renderToString(
      <FloorGrid tables={[]} canAdminister={false} onOpen={() => {}} onRename={() => {}} onToggleAvailability={() => {}} onCreate={() => {}} />,
    );
    expect(html).not.toContain("Thêm bàn");
  });

  it("renders a card per Table", () => {
    const html = renderToString(
      <FloorGrid tables={tables} canAdminister={false} onOpen={() => {}} onRename={() => {}} onToggleAvailability={() => {}} onCreate={() => {}} />,
    );
    expect(html).toContain("Bàn 1");
  });
});
