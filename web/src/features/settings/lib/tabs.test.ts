import { describe, expect, it } from "bun:test";
import { SETTINGS_CAPABILITIES, firstPermittedTab, permittedTabs, visiblePlannedTabs } from "./tabs";

const MANAGER = ["catalog.manage_availability", "catalog.administer_structure", "staff.administer", "sales.operate"];

describe("settings tabs", () => {
  it("lists availability for anyone who manages availability", () => {
    expect(firstPermittedTab(["catalog.manage_availability"])?.to).toBe("/settings/availability");
    expect(permittedTabs(["catalog.manage_availability"]).map((t) => t.label)).toEqual(["Kho & Món Tạm Hết"]);
  });

  it("gives a manager both routable tabs", () => {
    expect(permittedTabs(MANAGER).map((t) => t.to)).toEqual(["/settings/availability", "/settings/catalog"]);
  });

  it("has no tab for a session without a settings capability", () => {
    expect(firstPermittedTab(["sales.operate"])).toBeNull();
  });

  it("exposes every routable tab capability for the layout guard", () => {
    expect(SETTINGS_CAPABILITIES).toEqual(["catalog.manage_availability", "catalog.administer_structure"]);
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

  it("never grants access to the settings layout", () => {
    expect(SETTINGS_CAPABILITIES).not.toContain("staff.administer");
  });
});
