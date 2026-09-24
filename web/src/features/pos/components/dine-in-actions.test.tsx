import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { DineInActions } from "./dine-in-actions";
import type { DineInStatus } from "../utils/dine-in";

const idle: DineInStatus = {
  draftItemCount: 0,
  hasEditableDraft: true,
  hasUnsubmittedWork: false,
  openCheck: null,
  hasMultipleOpenChecks: false,
  progress: { done: 0, total: 0 },
  canClose: false,
  canOrder: true,
};

const noop = () => {};
function render(status: DineInStatus) {
  return renderToString(
    <DineInActions status={status} isShiftOpen isSending={false} isClosing={false} onSend={noop} onCollect={noop} onClose={noop} onLeave={noop} />,
  );
}

describe("DineInActions", () => {
  it("always offers the way back to the floor", () => {
    const html = render(idle);
    expect(html).toContain("Về sơ đồ bàn");
    expect(html).not.toContain("Gửi bếp");
    expect(html).not.toContain("Thu tiền");
    expect(html).not.toContain("Hoàn tất");
  });

  it("offers Gửi bếp for a drafted round", () => {
    expect(render({ ...idle, draftItemCount: 2 })).toContain("Gửi bếp (F9)");
  });

  it("disables Thu tiền while the draft has items, and says why", () => {
    const html = render({ ...idle, draftItemCount: 1, openCheck: { id: "c1", balance_vnd: 30_000 } });
    expect(html).toContain("Thu tiền");
    expect(html).toContain("Gửi bếp hoặc xóa món đang soạn trước khi thu tiền");
  });

  it("offers Thu tiền as the primary action once everything is sent", () => {
    expect(render({ ...idle, openCheck: { id: "c1", balance_vnd: 30_000 } })).toContain("Thu tiền (F9)");
  });

  it("offers Hoàn tất when the Session can close", () => {
    expect(render({ ...idle, canClose: true })).toContain("Hoàn tất (F9)");
  });
});
