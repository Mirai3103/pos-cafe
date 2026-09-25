import { describe, expect, it } from "bun:test";
import { visibleNavItems } from "./nav-items";

const MANAGER = [
  "catalog.view_prices", "catalog.manage_availability", "catalog.administer_structure", "catalog.change_price",
  "sales.operate", "sales_shift.operate", "preparation.operate", "staff.administer", "audit.inspect", "tables.administer",
];
const CASHIER = ["catalog.view_prices", "catalog.manage_availability", "sales.operate", "sales_shift.operate"];
const BARISTA = ["catalog.manage_availability", "preparation.operate"];

const labels = (held: string[]) => visibleNavItems(held).map((i) => i.label);

describe("visibleNavItems", () => {
  it("shows everything to a manager", () => {
    expect(labels(MANAGER)).toEqual(["Bán hàng", "Sơ đồ bàn", "Bếp KDS", "Ca làm việc", "Lịch sử", "Cài đặt"]);
  });

  it("hides the kitchen from a cashier", () => {
    expect(labels(CASHIER)).toEqual(["Bán hàng", "Sơ đồ bàn", "Ca làm việc", "Lịch sử", "Cài đặt"]);
  });

  it("gives a barista the kitchen and settings; history stays unguarded until slice 8", () => {
    expect(labels(BARISTA)).toEqual(["Bếp KDS", "Lịch sử", "Cài đặt"]);
  });
});
