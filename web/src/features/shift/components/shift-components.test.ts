import { describe, it, expect } from "bun:test";
import { NoActiveShiftView } from "./no-active-shift-view";
import { OpenShiftDialog } from "./open-shift-dialog";
import { OpenShiftDashboard } from "./open-shift-dashboard";
import { CashMovementDialog } from "./cash-movement-dialog";

describe("shift components", () => {
  it("exports NoActiveShiftView and OpenShiftDialog functions", () => {
    expect(typeof NoActiveShiftView).toBe("function");
    expect(typeof OpenShiftDialog).toBe("function");
    expect(typeof OpenShiftDashboard).toBe("function");
    expect(typeof CashMovementDialog).toBe("function");
  });
});
