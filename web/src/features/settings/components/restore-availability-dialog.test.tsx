import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { RestorePanel } from "./restore-availability-dialog";

const refs = [
  { kind: "item" as const, id: "i", name: "Cà phê sữa đá" },
  { kind: "size" as const, id: "s", name: "Cà phê đen (Size L)" },
];

describe("RestorePanel", () => {
  it("lists every entry it will restore", () => {
    const html = renderToString(<RestorePanel refs={refs} error={null} isPending={false} onConfirm={() => {}} onClose={() => {}} />);
    expect(html).toContain("Cà phê sữa đá");
    expect(html).toContain("Cà phê đen (Size L)");
    expect(html).toContain("Khôi phục 2 mục");
  });

  it("shows the error inline", () => {
    const html = renderToString(
      <RestorePanel refs={refs} error="Danh sách đã thay đổi, vui lòng kiểm tra lại" isPending={false} onConfirm={() => {}} onClose={() => {}} />,
    );
    expect(html).toContain("Danh sách đã thay đổi, vui lòng kiểm tra lại");
  });
});
