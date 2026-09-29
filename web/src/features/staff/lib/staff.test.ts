import { describe, expect, it } from "bun:test";
import {
  emptyForm,
  filterStaff,
  formFromRow,
  hasErrors,
  isFormChanged,
  isSelf,
  toggleRole,
  toStaffRows,
  validateForm,
  validatePin,
  type StaffRow,
} from "./staff";

const row = (over: Partial<StaffRow>): StaffRow => ({
  id: "id",
  displayName: "Tên",
  loginCode: "CODE",
  enabled: true,
  roles: ["BARISTA"],
  ...over,
});

describe("toStaffRows", () => {
  it("maps the API shape and drops unknown roles", () => {
    const rows = toStaffRows([
      { id: "a", display_name: "Lan", login_code: "LAN01", enabled: true, roles: ["MANAGER", "OWNER"] },
    ]);
    expect(rows).toEqual([{ id: "a", displayName: "Lan", loginCode: "LAN01", enabled: true, roles: ["MANAGER"] }]);
  });

  it("tolerates a null list", () => {
    expect(toStaffRows(null)).toEqual([]);
  });
});

describe("filterStaff", () => {
  const rows = [
    row({ id: "1", displayName: "Hà", loginCode: "HA02", enabled: false }),
    row({ id: "2", displayName: "Minh", loginCode: "MINH" }),
    row({ id: "3", displayName: "Đức", loginCode: "DUC" }),
  ];

  it("hides disabled accounts unless asked, and sorts enabled first then by name", () => {
    expect(filterStaff(rows, "", false).map((r) => r.id)).toEqual(["3", "2"]);
    expect(filterStaff(rows, "", true).map((r) => r.id)).toEqual(["3", "2", "1"]);
  });

  it("matches name without diacritics and login code case-insensitively", () => {
    expect(filterStaff(rows, "duc", false).map((r) => r.id)).toEqual(["3"]);
    expect(filterStaff(rows, "minh", false).map((r) => r.id)).toEqual(["2"]);
    expect(filterStaff(rows, "ha02", true).map((r) => r.id)).toEqual(["1"]);
  });
});

describe("validatePin", () => {
  it("requires 4-8 digits and a matching confirmation", () => {
    expect(validatePin("12", "12").pin).toBe("PIN gồm 4–8 chữ số");
    expect(validatePin("12a4", "12a4").pin).toBe("PIN gồm 4–8 chữ số");
    expect(validatePin("1234", "1235").pinConfirm).toBe("PIN xác nhận không khớp");
    expect(hasErrors(validatePin("123456", "123456"))).toBe(false);
  });
});

describe("validateForm", () => {
  it("requires name, code, and a role", () => {
    const errors = validateForm({ ...emptyForm(), displayName: "  ", loginCode: "", roles: [] }, "edit");
    expect(errors.displayName).toBe("Vui lòng nhập tên");
    expect(errors.loginCode).toBe("Vui lòng nhập mã đăng nhập");
    expect(errors.roles).toBe("Chọn ít nhất một vai trò");
  });

  it("limits name to 120 and code to 24 characters after trim", () => {
    const errors = validateForm({ ...emptyForm(), displayName: "a".repeat(121), loginCode: "A".repeat(25), roles: ["CASHIER"] }, "edit");
    expect(errors.displayName).toBe("Tên tối đa 120 ký tự");
    expect(errors.loginCode).toBe("Mã đăng nhập tối đa 24 ký tự");
  });

  it("checks the PIN only when creating", () => {
    const form = { ...emptyForm(), displayName: "Minh", loginCode: "MINH", roles: ["CASHIER" as const] };
    expect(hasErrors(validateForm(form, "edit"))).toBe(false);
    expect(validateForm(form, "create").pin).toBe("PIN gồm 4–8 chữ số");
  });
});

describe("isFormChanged", () => {
  const r = row({ displayName: "Minh", loginCode: "MINH", roles: ["CASHIER", "BARISTA"] });

  it("is false for an untouched form, ignoring role order, spacing, and code case", () => {
    expect(isFormChanged({ ...formFromRow(r), roles: ["BARISTA", "CASHIER"], loginCode: " minh ", displayName: "Minh " }, r)).toBe(false);
  });

  it("is true when any field differs", () => {
    expect(isFormChanged({ ...formFromRow(r), displayName: "Minh Mới" }, r)).toBe(true);
    expect(isFormChanged({ ...formFromRow(r), roles: ["CASHIER"] }, r)).toBe(true);
  });
});

describe("self rules and roles", () => {
  it("recognizes the signed-in row", () => {
    expect(isSelf(row({ id: "me" }), "me")).toBe(true);
    expect(isSelf(row({ id: "x" }), "me")).toBe(false);
    expect(isSelf(null, "me")).toBe(false);
  });

  it("toggles a role and keeps canonical order", () => {
    expect(toggleRole(["BARISTA"], "MANAGER")).toEqual(["MANAGER", "BARISTA"]);
    expect(toggleRole(["MANAGER", "BARISTA"], "MANAGER")).toEqual(["BARISTA"]);
  });
});
