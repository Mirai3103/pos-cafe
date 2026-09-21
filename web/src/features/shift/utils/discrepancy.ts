import type {
  ShiftCloseDiscrepancyInputDimension,
  ShiftCloseDiscrepancyInputReason,
  ShiftReconciliationPreviewEntry,
} from "@/api/generated/models";

export type {
  ShiftCloseDiscrepancyInputDimension,
  ShiftCloseDiscrepancyInputReason,
};

export const DIMENSION_LABELS: Record<ShiftCloseDiscrepancyInputDimension, string> = {
  CASH: "Tiền mặt",
  MANUAL_QR_RECEIVED: "VietQR Đã Nhận",
  MANUAL_QR_REFUNDED: "VietQR Hoàn Tiền",
};

export const REASON_LABELS: Record<ShiftCloseDiscrepancyInputReason, string> = {
  CASH_COUNT_DIFFERENCE: "Chênh lệch tiền mặt kiểm đếm",
  QR_OBSERVATION_DIFFERENCE: "Chênh lệch đối soát VietQR",
  UNEXPLAINED: "Chưa rõ nguyên nhân",
};

export function deriveNonZeroDimensions(
  dimensions: ShiftReconciliationPreviewEntry[]
): ShiftReconciliationPreviewEntry[] {
  return dimensions.filter(
    (d) => d.difference_vnd !== undefined && d.difference_vnd !== null && d.difference_vnd !== 0
  );
}
