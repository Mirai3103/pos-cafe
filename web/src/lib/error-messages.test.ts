import { describe, expect, it } from "bun:test";
import { ApiError } from "./unwrap";
import { messageForError } from "./error-messages";

describe("messageForError", () => {
  it("maps a known code to its Vietnamese message", () => {
    expect(messageForError(new ApiError(401, "UNAUTHORIZED", "unauthorized"))).toBe(
      "Phiên đăng nhập không hợp lệ hoặc đã hết hạn.",
    );
  });

  it("maps sales shift and draft error codes to Vietnamese messages", () => {
    expect(
      messageForError(new ApiError(409, "OPEN_SALES_SHIFT_REQUIRED", "shift required")),
    ).toBe("Ca bán hàng chưa được mở. Vui lòng mở ca trước khi tạo đơn.");

    expect(
      messageForError(new ApiError(404, "MENU_ITEM_NOT_FOUND", "item not found")),
    ).toBe("Món không có trong thực đơn.");

    expect(
      messageForError(new ApiError(400, "INVALID_PREPARATION_NOTE", "invalid note")),
    ).toBe("Ghi chú pha chế không hợp lệ (tối đa 200 ký tự).");

    expect(
      messageForError(new ApiError(400, "INVALID_QUANTITY", "invalid qty")),
    ).toBe("Số lượng món phải từ 1 đến 9999.");
  });

  it("falls back to the server message for an unmapped code", () => {
    expect(messageForError(new ApiError(409, "SHIFT_ALREADY_OPEN", "ca làm việc đang mở"))).toBe(
      "ca làm việc đang mở",
    );
  });

  it("returns a generic message for a non-ApiError", () => {
    expect(messageForError(new Error("socket hang up"))).toBe(
      "Không kết nối được máy chủ. Kiểm tra mạng nội bộ rồi thử lại.",
    );
  });
});
