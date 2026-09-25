import { describe, expect, it } from "bun:test";
import { RESTORE_SUCCESS_MESSAGE, toggleSuccessMessage } from "./stock-messages";

describe("stock toast messages", () => {
  it("follows the design's wording", () => {
    expect(toggleSuccessMessage("Bạc xỉu", false)).toBe('Đã đánh dấu "Bạc xỉu" TẠM HẾT HÀNG');
    expect(toggleSuccessMessage("Bạc xỉu", true)).toBe('Đã mở bán lại món "Bạc xỉu" (Còn hàng)');
    expect(RESTORE_SUCCESS_MESSAGE).toBe("Đã khôi phục toàn bộ danh mục về trạng thái Còn hàng!");
  });
});
