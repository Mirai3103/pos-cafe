// web/src/features/catalog/lib/validation.ts
import type { CategoryForm, GroupForm, ItemForm } from "./forms";

export type FieldErrors = Record<string, string>;

export const MAX_VND = 2_147_483_647;
export const MAX_DESCRIPTION = 300;
const CODE_PATTERN = /^[a-z0-9]{1,12}$/;

/** The server compares names trimmed and lowercased. */
export function nameKey(name: string): string {
  return name.trim().toLocaleLowerCase("vi");
}

export function hasErrors(errors: FieldErrors): boolean {
  return Object.keys(errors).length > 0;
}

const isPrice = (v: number) => Number.isInteger(v) && v >= 1 && v <= MAX_VND;
const isSurcharge = (v: number) => Number.isInteger(v) && v >= 0 && v <= MAX_VND;

function duplicateKeys(rows: readonly { key: string; name: string }[]): Set<string> {
  const first = new Map<string, string>();
  const dup = new Set<string>();
  for (const row of rows) {
    const k = nameKey(row.name);
    if (!k) continue;
    const seen = first.get(k);
    if (seen) {
      dup.add(seen);
      dup.add(row.key);
    } else {
      first.set(k, row.key);
    }
  }
  return dup;
}

export function validateItemForm(form: ItemForm): FieldErrors {
  const errors: FieldErrors = {};
  if (!form.name.trim()) errors.name = "Vui lòng nhập tên món";
  if (!form.categoryId) errors.categoryId = "Vui lòng chọn danh mục";
  if (form.mode === "single") {
    if (!isPrice(form.priceVnd)) errors.priceVnd = "Giá bán phải từ 1 VND";
  } else {
    if (form.sizes.length === 0) errors.sizes = "Cần ít nhất một kích cỡ";
    const dup = duplicateKeys(form.sizes);
    for (const s of form.sizes) {
      const key = `size.${s.key}`;
      if (!s.name.trim()) errors[key] = "Vui lòng nhập tên kích cỡ";
      else if (dup.has(s.key)) errors[key] = "Tên kích cỡ bị trùng";
      else if (!isPrice(s.priceVnd)) errors[key] = "Giá bán phải từ 1 VND";
    }
  }
  const code = form.code.trim().toLowerCase();
  if (code && !CODE_PATTERN.test(code)) errors.code = "Mã chỉ gồm chữ cái không dấu và số, tối đa 12 ký tự";
  if (Array.from(form.description.trim()).length > MAX_DESCRIPTION) errors.description = "Mô tả tối đa 300 ký tự";
  return errors;
}

export function validateCategoryForm(form: CategoryForm): FieldErrors {
  const errors: FieldErrors = {};
  if (!form.name.trim()) errors.name = "Vui lòng nhập tên danh mục";
  if (!Number.isInteger(form.displayOrder) || form.displayOrder < 0 || form.displayOrder > 9999) {
    errors.displayOrder = "Thứ tự hiển thị từ 0 đến 9999";
  }
  return errors;
}

export function validateGroupForm(form: GroupForm): FieldErrors {
  const errors: FieldErrors = {};
  if (!form.name.trim()) errors.name = "Vui lòng nhập tên nhóm";
  if (form.rows.length === 0) errors.rows = "Cần ít nhất một lựa chọn";
  const dup = duplicateKeys(form.rows);
  for (const r of form.rows) {
    const key = `row.${r.key}`;
    if (!r.name.trim()) errors[key] = "Vui lòng nhập tên lựa chọn";
    else if (dup.has(r.key)) errors[key] = "Tên lựa chọn bị trùng";
    else if (!isSurcharge(r.surchargeVnd)) errors[key] = "Giá thêm phải từ 0 VND";
  }
  const { min, max } = form;
  if (!Number.isInteger(max) || max < 1 || max > form.rows.length) errors.max = "Tối đa phải từ 1 đến số lựa chọn";
  if (!Number.isInteger(min) || min < 0 || min > max) errors.min = "Tối thiểu phải từ 0 đến tối đa";
  const defaults = form.rows.filter((r) => r.isDefault).length;
  if (defaults < min || defaults > max) errors.defaults = "Số lựa chọn mặc định phải từ tối thiểu đến tối đa";
  return errors;
}
