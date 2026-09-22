import { describe, it, expect } from "bun:test";
import {
  useCurrentShift,
  useOpenShift,
  useRecordCashMovement,
  useStartReconciliation,
  useRecordCashCount,
  useRecordQRObservation,
  useCloseShift,
} from "./use-shift";

describe("use-shift api seam", () => {
  it("exports all required shift hook functions", () => {
    expect(typeof useCurrentShift).toBe("function");
    expect(typeof useOpenShift).toBe("function");
    expect(typeof useRecordCashMovement).toBe("function");
    expect(typeof useStartReconciliation).toBe("function");
    expect(typeof useRecordCashCount).toBe("function");
    expect(typeof useRecordQRObservation).toBe("function");
    expect(typeof useCloseShift).toBe("function");
  });
});
