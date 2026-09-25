import { describe, expect, it } from "bun:test";
import { ApiError } from "./unwrap";
import { ERROR_MESSAGES, messageForError } from "./error-messages";

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

  it("maps the submit and closure codes to Vietnamese messages", () => {
    const cases: Array<[string, string]> = [
      ["CHECK_NOT_SETTLED_FOR_SUBMISSION", "Đơn chưa thu đủ tiền, chưa thể gửi bếp."],
      ["UNFULFILLED_PREPARATION_FOR_CLOSURE", "Bếp chưa hoàn tất tất cả món."],
      ["UNSUBMITTED_WORK_FOR_CLOSURE", "Còn món chưa gửi bếp."],
      ["CHECK_NOT_SETTLED_FOR_CLOSURE", "Đơn chưa thu đủ tiền."],
      ["ORDER_REQUIRED_FOR_CLOSURE", "Đơn chưa có món nào được gửi bếp."],
      ["PENDING_REFUND_FOR_CLOSURE", "Đơn còn khoản hoàn tiền chưa xử lý. Vui lòng báo quản lý."],
      ["COMPLETED_SALE_NOT_FOUND", "Không tìm thấy hóa đơn hoàn tất."],
      ["NOTHING_TO_SUBMIT", "Đơn này đã được gửi bếp."],
    ];
    for (const [code, message] of cases) {
      expect(messageForError(new ApiError(409, code, code))).toBe(message);
    }
  });
});

describe("commit and payment error codes", () => {
  const codes = [
    "EMPTY_DRAFT",
    "COMMIT_MENU_ITEM_UNAVAILABLE",
    "COMMIT_MENU_ITEM_RETIRED",
    "COMMIT_SIZE_REQUIRED",
    "COMMIT_SIZE_INVALID",
    "COMMIT_SIZE_UNAVAILABLE",
    "COMMIT_SIZE_RETIRED",
    "COMMIT_MODIFIER_OPTION_INVALID",
    "COMMIT_MODIFIER_OPTION_UNAVAILABLE",
    "COMMIT_MODIFIER_OPTION_RETIRED",
    "COMMIT_MODIFIER_GROUP_INVALID",
    "COMMIT_MODIFIER_GROUP_RETIRED",
    "NEW_ORDER_DRAFT_NOT_AVAILABLE",
    "CHECK_NOT_FOUND",
    "CHECK_NOT_OPEN",
    "CHECK_HAS_PAYMENT",
    "PAYMENT_EXCEEDS_CHECK_BALANCE",
    "INSUFFICIENT_CASH_TENDERED",
    "REQUEST_CONFLICT",
  ];

  it("translates every commit and payment code", () => {
    for (const code of codes) {
      expect(ERROR_MESSAGES[code]).toBeString();
      expect(ERROR_MESSAGES[code].length).toBeGreaterThan(0);
    }
  });

  it("reads the payment shortfall message through messageForError", () => {
    const error = new ApiError(409, "INSUFFICIENT_CASH_TENDERED", "cash tendered is below");
    expect(messageForError(error)).toBe("Tiền khách đưa ít hơn số tiền cần thu.");
  });

  it("reads the request-id replay conflict message through messageForError", () => {
    const error = new ApiError(409, "REQUEST_CONFLICT", "request_id reused with a different fingerprint");
    expect(messageForError(error)).toBe(
      "Yêu cầu này đã được gửi với số tiền khác. Vui lòng đóng và thử lại.",
    );
  });
});

describe("slice 7 error codes", () => {
  /** Pinned against internal/tables/errors.go and internal/sales/errors.go. */
  it("has a Vietnamese message for every Table and dine-in code", () => {
    for (const code of [
      "TABLE_NOT_FOUND",
      "TABLE_NAME_CONFLICT",
      "DINE_IN_TABLE_SELECTION_REQUIRED",
      "DINE_IN_TABLE_SELECTION_DUPLICATE",
      "DINE_IN_TABLE_NOT_FOUND",
      "DINE_IN_TABLE_UNAVAILABLE",
      "TAKEAWAY_TABLE_ASSIGNMENT_NOT_AVAILABLE",
      "NEW_ORDER_DRAFT_NOT_AVAILABLE",
    ]) {
      expect(ERROR_MESSAGES[code]).toBeTruthy();
    }
  });
});
