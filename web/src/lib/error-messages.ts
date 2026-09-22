import { ApiError } from "./unwrap";

/** Stable codes emitted by internal/response/response.go. */
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
};

const NETWORK_MESSAGE = "Không kết nối được máy chủ. Kiểm tra mạng nội bộ rồi thử lại.";

export function messageForError(error: unknown): string {
  if (error instanceof ApiError) {
    return ERROR_MESSAGES[error.code] ?? error.message;
  }
  return NETWORK_MESSAGE;
}
