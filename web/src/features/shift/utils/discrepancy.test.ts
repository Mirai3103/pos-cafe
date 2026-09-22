import { describe, expect, it } from "bun:test";
import {
  DIMENSION_LABELS,
  REASON_LABELS,
  deriveNonZeroDimensions,
  getReasonsForDimension,
  getDefaultReasonForDimension,
} from "./discrepancy";

describe("discrepancy utilities", () => {
  it("translates dimensions to Vietnamese", () => {
    expect(DIMENSION_LABELS.CASH).toBe("Tiền mặt");
    expect(DIMENSION_LABELS.MANUAL_QR_RECEIVED).toBe("VietQR Đã Nhận");
    expect(DIMENSION_LABELS.MANUAL_QR_REFUNDED).toBe("VietQR Hoàn Tiền");
  });

  it("translates reasons to Vietnamese", () => {
    expect(REASON_LABELS.CASH_COUNT_DIFFERENCE).toBe("Chênh lệch tiền mặt kiểm đếm");
    expect(REASON_LABELS.QR_OBSERVATION_DIFFERENCE).toBe("Chênh lệch đối soát VietQR");
    expect(REASON_LABELS.UNEXPLAINED).toBe("Chưa rõ nguyên nhân");
    expect(REASON_LABELS.OTHER).toBe("Lý do khác (cần ghi chú)");
  });

  it("returns allowed reasons for dimensions according to domain rules", () => {
    expect(getReasonsForDimension("CASH")).toEqual([
      "CASH_COUNT_DIFFERENCE",
      "UNEXPLAINED",
      "OTHER",
    ]);
    expect(getReasonsForDimension("MANUAL_QR_RECEIVED")).toEqual([
      "QR_OBSERVATION_DIFFERENCE",
      "UNEXPLAINED",
      "OTHER",
    ]);
    expect(getReasonsForDimension("MANUAL_QR_REFUNDED")).toEqual([
      "QR_OBSERVATION_DIFFERENCE",
      "UNEXPLAINED",
      "OTHER",
    ]);
  });

  it("returns appropriate default reasons per dimension", () => {
    expect(getDefaultReasonForDimension("CASH")).toBe("CASH_COUNT_DIFFERENCE");
    expect(getDefaultReasonForDimension("MANUAL_QR_RECEIVED")).toBe("QR_OBSERVATION_DIFFERENCE");
    expect(getDefaultReasonForDimension("MANUAL_QR_REFUNDED")).toBe("QR_OBSERVATION_DIFFERENCE");
  });

  it("derives only dimensions with nonzero difference", () => {
    const dimensions = [
      { dimension: "CASH" as const, expected_vnd: 1000, observed_vnd: 900, difference_vnd: -100, recheck_required: false },
      { dimension: "MANUAL_QR_RECEIVED" as const, expected_vnd: 500, observed_vnd: 500, difference_vnd: 0, recheck_required: false },
      { dimension: "MANUAL_QR_REFUNDED" as const, expected_vnd: 0, observed_vnd: 50, difference_vnd: 50, recheck_required: false },
    ];
    const nonZero = deriveNonZeroDimensions(dimensions);
    expect(nonZero.map((d) => d.dimension)).toEqual(["CASH", "MANUAL_QR_REFUNDED"]);
  });
});

