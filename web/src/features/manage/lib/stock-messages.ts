/** Toast copy for availability changes, verbatim from the design prototype. */
export function toggleSuccessMessage(name: string, available: boolean): string {
  return available ? `Đã mở bán lại món "${name}" (Còn hàng)` : `Đã đánh dấu "${name}" TẠM HẾT HÀNG`;
}

export const RESTORE_SUCCESS_MESSAGE = "Đã khôi phục toàn bộ danh mục về trạng thái Còn hàng!";
