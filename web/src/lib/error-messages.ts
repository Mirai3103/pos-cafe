import { ApiError } from "./unwrap";

/** Stable codes emitted by internal/response/response.go and internal/sales/errors.go. */
export const ERROR_MESSAGES: Record<string, string> = {
  UNAUTHORIZED: "Phiên đăng nhập không hợp lệ hoặc đã hết hạn.",
  FORBIDDEN: "Bạn không có quyền thực hiện thao tác này.",
  BAD_REQUEST: "Dữ liệu gửi lên không hợp lệ.",
  NOT_FOUND: "Không tìm thấy dữ liệu.",
  CONFLICT: "Thao tác xung đột với trạng thái hiện tại.",
  TOO_MANY_REQUESTS: "Bạn thử quá nhiều lần. Chờ một lát rồi thử lại.",
  INTERNAL_ERROR: "Máy chủ gặp sự cố. Báo quản lý nếu tình trạng tiếp diễn.",
  EMPTY_RESPONSE: "Máy chủ trả về dữ liệu rỗng.",
  SHIFT_CASH_RECOUNT_REQUIRED: "Ca có chênh lệch tiền mặt cần ít nhất 2 lần kiểm đếm trước khi đóng ca.",
  SHIFT_QR_RECHECK_REQUIRED: "Ca có chênh lệch VietQR cần ít nhất 2 lần cập nhật đối soát trước khi đóng ca.",
  SHIFT_DISCREPANCY_REASON_REQUIRED: "Vui lòng chọn lý do cho tất cả các khoản chênh lệch.",
  SHIFT_DISCREPANCY_REASON_UNEXPECTED: "Lý do chênh lệch không khớp với số liệu đối soát.",
  MANAGER_APPROVAL_UNAVAILABLE: "Không thể xác thực Quản lý. Vui lòng kiểm tra mã đăng nhập và PIN.",
  OPEN_SALES_SHIFT_REQUIRED: "Ca bán hàng chưa được mở. Vui lòng mở ca trước khi tạo đơn.",
  SERVICE_SESSION_NOT_FOUND: "Phiên phục vụ không tồn tại hoặc đã bị xóa.",
  SERVICE_SESSION_ALREADY_CLOSED: "Phiên phục vụ này đã kết thúc.",
  EDITABLE_DRAFT_NOT_FOUND: "Không tìm thấy đơn nháp hợp lệ để chỉnh sửa.",
  DRAFT_ITEM_NOT_FOUND: "Món trong đơn không tồn tại hoặc đã bị xóa.",
  MENU_ITEM_NOT_FOUND: "Món không có trong thực đơn.",
  MENU_ITEM_UNAVAILABLE: "Món này hiện đang tạm ngưng phục vụ.",
  MENU_ITEM_RETIRED: "Món này đã ngừng kinh doanh.",
  SIZE_NOT_FOUND: "Kích cỡ món không hợp lệ.",
  SIZE_UNAVAILABLE: "Kích cỡ đã chọn hiện đang tạm hết.",
  SIZE_RETIRED: "Kích cỡ này đã ngừng phục vụ.",
  MODIFIER_OPTION_NOT_FOUND: "Tùy chọn topping không tồn tại.",
  MODIFIER_OPTION_UNAVAILABLE: "Tùy chọn topping đã chọn hiện đang tạm hết.",
  MODIFIER_OPTION_RETIRED: "Tùy chọn topping này đã ngừng phục vụ.",
  INVALID_PREPARATION_NOTE: "Ghi chú pha chế không hợp lệ (tối đa 200 ký tự).",
  INVALID_QUANTITY: "Số lượng món phải từ 1 đến 9999.",
};

const NETWORK_MESSAGE = "Không kết nối được máy chủ. Kiểm tra mạng nội bộ rồi thử lại.";

export function messageForError(error: unknown): string {
  if (error instanceof ApiError) {
    return ERROR_MESSAGES[error.code] ?? error.message;
  }
  return NETWORK_MESSAGE;
}
