import { describe, expect, it } from "bun:test";
import { MANAGE_CAPABILITIES, firstPermittedTab, permittedTabs, visiblePlannedTabs } from "./tabs";

const MANAGER = ["catalog.manage_availability", "catalog.administer_structure", "staff.administer", "sales.operate"];

describe("manage tabs", () => {
  it("lists availability for anyone who manages availability", () => {
    expect(firstPermittedTab(["catalog.manage_availability"])?.to).toBe("/manage/availability");
    expect(permittedTabs(["catalog.manage_availability"]).map((t) => t.label)).toEqual(["Kho & Món Tạm Hết"]);
  });

  it("gives a manager the three routable tabs", () => {
    expect(permittedTabs(MANAGER).map((t) => t.to)).toEqual(["/manage/availability", "/manage/catalog", "/manage/staff"]);
  });

  it("gives a cashier or barista no staff tab", () => {
    expect(permittedTabs(["catalog.manage_availability", "sales.operate"]).map((t) => t.to)).not.toContain("/manage/staff");
  });

  it("has no tab for a session without a manage capability", () => {
    expect(firstPermittedTab(["sales.operate"])).toBeNull();
  });

  it("exposes every routable tab capability for the layout guard", () => {
    expect(MANAGE_CAPABILITIES).toEqual(["catalog.manage_availability", "catalog.administer_structure", "staff.administer"]);
  });
});

describe("planned tabs", () => {
  it("shows a manager the tab still to come", () => {
    expect(visiblePlannedTabs(MANAGER).map((t) => t.label)).toEqual(["Thông tin Quán & VietQR"]);
  });

  it("shows a barista or cashier none of them", () => {
    expect(visiblePlannedTabs(["catalog.manage_availability", "preparation.operate"])).toEqual([]);
    expect(visiblePlannedTabs(["catalog.manage_availability", "sales.operate", "sales_shift.operate"])).toEqual([]);
  });
});
