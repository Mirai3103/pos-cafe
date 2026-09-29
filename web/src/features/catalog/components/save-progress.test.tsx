// web/src/features/catalog/components/save-progress.test.tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { SAVE_INCOMPLETE, SaveProgress } from "./save-progress";

describe("SaveProgress", () => {
  it("renders nothing before a save or after a full success", () => {
    expect(renderToString(<SaveProgress results={null} />)).toBe("");
    expect(renderToString(<SaveProgress results={[{ label: "Đổi tên món", type: "item.rename", status: "done" }]} />)).toBe("");
  });

  it("lists each step with the failure message", () => {
    const html = renderToString(
      <SaveProgress
        results={[
          { label: "Đổi tên món", type: "item.rename", status: "done" },
          { label: "Đổi giá món", type: "item.reprice", status: "failed", error: "Mã PIN sai" },
          { label: "Cập nhật nhóm topping", type: "item.groups", status: "pending" },
        ]}
      />,
    );
    expect(html).toContain(SAVE_INCOMPLETE);
    expect(html).toContain("Đổi giá món");
    expect(html).toContain("Mã PIN sai");
    expect(html).toContain("Cập nhật nhóm topping");
  });
});
