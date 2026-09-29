import { describe, expect, it } from "bun:test";
import { SETTINGS_CAPABILITIES, firstPermittedTab, permittedTabs, visiblePlannedTabs } from "./tabs";

const MANAGER = ["catalog.manage_availability", "catalog.administer_structure", "staff.administer", "sales.operate"];

describe("settings tabs", () => {
  it("lists availability for anyone who manages availability", () => {
    expect(firstPermittedTab(["catalog.manage_availability"])?.to).toBe("/settings/availability");
    expect(permittedTabs(["catalog.manage_availability"]).map((t) => t.label)).toEqual(["Kho & Món Tạm Hết"]);
  });

  it("gives a manager the three routable tabs", () => {
    expect(permittedTabs(MANAGER).map((t) => t.to)).toEqual(["/settings/availability", "/settings/catalog", "/settings/staff"]);
  });

  it("gives a cashier or barista no staff tab", () => {
    expect(permittedTabs(["catalog.manage_availability", "sales.operate"]).map((t) => t.to)).not.toContain("/settings/staff");
  });

  it("has no tab for a session without a settings capability", () => {
    expect(firstPermittedTab(["sales.operate"])).toBeNull();
  });

  it("exposes every routable tab capability for the layout guard", () => {
    expect(SETTINGS_CAPABILITIES).toEqual(["catalog.manage_availability", "catalog.administer_structure", "staff.administer"]);
  });
});

describe("planned tabs", () => {
  it("shows a manager the two tabs still to come", () => {
    expect(visiblePlannedTabs(MANAGER).map((t) => t.label)).toEqual(["Thông tin Quán & VietQR", "Tùy chỉnh In & Hệ thống"]);
  });

  it("shows a barista or cashier none of them", () => {
    expect(visiblePlannedTabs(["catalog.manage_availability", "preparation.operate"])).toEqual([]);
    expect(visiblePlannedTabs(["catalog.manage_availability", "sales.operate", "sales_shift.operate"])).toEqual([]);
  });
});
