// web/src/features/catalog/lib/validation.test.ts
import { describe, expect, it } from "bun:test";
import { groupFormFrom, itemFormFrom, type GroupForm, type ItemForm } from "./forms";
import { hasErrors, validateCategoryForm, validateGroupForm, validateItemForm } from "./validation";

const validItem: ItemForm = { ...itemFormFrom(null, "c-tea"), name: "Trà đào", priceVnd: 45000 };

function group(patch: Partial<GroupForm>): GroupForm {
  return {
    ...groupFormFrom(null),
    name: "Topping",
    rows: [
      { key: "a", id: null, name: "Trân châu", surchargeVnd: 10000, isDefault: false },
      { key: "b", id: null, name: "Thạch", surchargeVnd: 0, isDefault: false },
    ],
    min: 0,
    max: 2,
    ...patch,
  };
}

describe("validateItemForm", () => {
  it("accepts a valid single-price item", () => {
    expect(validateItemForm(validItem)).toEqual({});
  });

  it("requires a name, a category, and a positive price", () => {
    const errors = validateItemForm({ ...validItem, name: "  ", categoryId: "", priceVnd: 0 });
    expect(Object.keys(errors).sort()).toEqual(["categoryId", "name", "priceVnd"]);
  });

  it("requires at least one size and checks each row", () => {
    expect(validateItemForm({ ...validItem, mode: "sizes", sizes: [] }).sizes).toBe("Cần ít nhất một kích cỡ");
    const errors = validateItemForm({
      ...validItem,
      mode: "sizes",
      sizes: [
        { key: "a", id: null, name: "M", priceVnd: 30000 },
        { key: "b", id: null, name: " m ", priceVnd: 35000 },
        { key: "c", id: null, name: "L", priceVnd: 1.5 },
      ],
    });
    expect(errors["size.a"]).toBe("Tên kích cỡ bị trùng");
    expect(errors["size.b"]).toBe("Tên kích cỡ bị trùng");
    expect(errors["size.c"]).toBe("Giá bán phải từ 1 VND");
  });

  it("checks the code pattern after lowercasing", () => {
    expect(validateItemForm({ ...validItem, code: "CFSD" })).toEqual({});
    expect(validateItemForm({ ...validItem, code: "cà-phê" }).code).toBeDefined();
    expect(validateItemForm({ ...validItem, code: "a".repeat(13) }).code).toBeDefined();
  });

  it("limits the description to 300 characters", () => {
    expect(validateItemForm({ ...validItem, description: "đ".repeat(300) })).toEqual({});
    expect(validateItemForm({ ...validItem, description: "đ".repeat(301) }).description).toBe("Mô tả tối đa 300 ký tự");
  });
});

describe("validateCategoryForm", () => {
  it("requires a name and an order from 0 to 9999", () => {
    expect(validateCategoryForm({ name: "Trà", icon: null, displayOrder: 0, groupIds: [] })).toEqual({});
    const errors = validateCategoryForm({ name: "", icon: null, displayOrder: 10000, groupIds: [] });
    expect(Object.keys(errors).sort()).toEqual(["displayOrder", "name"]);
  });
});

describe("validateGroupForm", () => {
  it("accepts a valid group", () => {
    expect(validateGroupForm(group({}))).toEqual({});
  });

  it("requires rows with unique, non-empty names and non-negative surcharges", () => {
    expect(validateGroupForm(group({ rows: [] })).rows).toBe("Cần ít nhất một lựa chọn");
    const errors = validateGroupForm(
      group({
        rows: [
          { key: "a", id: null, name: "Thạch", surchargeVnd: 0, isDefault: false },
          { key: "b", id: null, name: "thạch", surchargeVnd: 0, isDefault: false },
          { key: "c", id: null, name: "", surchargeVnd: -1, isDefault: false },
        ],
        max: 3,
      }),
    );
    expect(errors["row.a"]).toBe("Tên lựa chọn bị trùng");
    expect(errors["row.c"]).toBe("Vui lòng nhập tên lựa chọn");
  });

  it("bounds max by the row count and min by max", () => {
    expect(validateGroupForm(group({ max: 3 })).max).toBe("Tối đa phải từ 1 đến số lựa chọn");
    expect(validateGroupForm(group({ max: 0 })).max).toBe("Tối đa phải từ 1 đến số lựa chọn");
    expect(validateGroupForm(group({ min: 3, max: 2 })).min).toBe("Tối thiểu phải từ 0 đến tối đa");
  });

  it("keeps the default count between min and max", () => {
    expect(validateGroupForm(group({ min: 1 })).defaults).toBe("Số lựa chọn mặc định phải từ tối thiểu đến tối đa");
    const both = group({ max: 1, rows: group({}).rows.map((r) => ({ ...r, isDefault: true })) });
    expect(validateGroupForm(both).defaults).toBeDefined();
  });

  it("reports whether any error exists", () => {
    expect(hasErrors({})).toBe(false);
    expect(hasErrors({ name: "x" })).toBe(true);
  });
});
