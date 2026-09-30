import type { ShiftReconciliationPreviewEntry } from "@/api/generated/models";

/**
 * Closure dimensions and catalogued reasons, mirroring internal/shift/domain.go.
 * The OpenAPI spec types them as plain strings, so the unions live here.
 */
export type ShiftCloseDiscrepancyInputDimension =
  | "CASH"
  | "MANUAL_QR_RECEIVED"
  | "MANUAL_QR_REFUNDED";

export type ShiftCloseDiscrepancyInputReason =
  | "CASH_COUNT_DIFFERENCE"
  | "QR_OBSERVATION_DIFFERENCE"
  | "UNEXPLAINED"
  | "OTHER";

export const DIMENSION_LABELS: Record<ShiftCloseDiscrepancyInputDimension, string> = {
  CASH: "Tiền mặt",
  MANUAL_QR_RECEIVED: "VietQR Đã Nhận",
  MANUAL_QR_REFUNDED: "VietQR Hoàn Tiền",
};

export const REASON_LABELS: Record<ShiftCloseDiscrepancyInputReason, string> = {
  CASH_COUNT_DIFFERENCE: "Chênh lệch tiền mặt kiểm đếm",
  QR_OBSERVATION_DIFFERENCE: "Chênh lệch đối soát VietQR",
  UNEXPLAINED: "Chưa rõ nguyên nhân",
  OTHER: "Lý do khác (cần ghi chú)",
};

export function getReasonsForDimension(
  dimension: ShiftCloseDiscrepancyInputDimension
): ShiftCloseDiscrepancyInputReason[] {
  if (dimension === "CASH") {
    return ["CASH_COUNT_DIFFERENCE", "UNEXPLAINED", "OTHER"];
  }
  return ["QR_OBSERVATION_DIFFERENCE", "UNEXPLAINED", "OTHER"];
}

export function getDefaultReasonForDimension(
  dimension: ShiftCloseDiscrepancyInputDimension
): ShiftCloseDiscrepancyInputReason {
  if (dimension === "CASH") {
    return "CASH_COUNT_DIFFERENCE";
  }
  return "QR_OBSERVATION_DIFFERENCE";
}

export function deriveNonZeroDimensions(
  dimensions: ShiftReconciliationPreviewEntry[]
): ShiftReconciliationPreviewEntry[] {
  return dimensions.filter(
    (d) => d.difference_vnd !== undefined && d.difference_vnd !== null && d.difference_vnd !== 0
  );
}
