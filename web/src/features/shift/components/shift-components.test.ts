import { describe, it, expect } from "bun:test";
import { NoActiveShiftView } from "./no-active-shift-view";
import { OpenShiftDialog } from "./open-shift-dialog";
import { OpenShiftDashboard } from "./open-shift-dashboard";
import { CashMovementDialog } from "./cash-movement-dialog";
import { ClosingReconciliationView } from "./closing-reconciliation-view";
import { CashCountDialog } from "./cash-count-dialog";
import { QRObservationDialog } from "./qr-observation-dialog";
import { CloseShiftDialog } from "./close-shift-dialog";

describe("shift components", () => {
  it("exports shift view and dialog component functions", () => {
    expect(typeof NoActiveShiftView).toBe("function");
    expect(typeof OpenShiftDialog).toBe("function");
    expect(typeof OpenShiftDashboard).toBe("function");
    expect(typeof CashMovementDialog).toBe("function");
    expect(typeof ClosingReconciliationView).toBe("function");
    expect(typeof CashCountDialog).toBe("function");
    expect(typeof QRObservationDialog).toBe("function");
    expect(typeof CloseShiftDialog).toBe("function");
  });
});
