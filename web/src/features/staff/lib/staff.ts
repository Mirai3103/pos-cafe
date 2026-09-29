import type { AuthStaffDetailResponse } from "@/api/generated/models";
import { normalizeVietnamese } from "@/lib/search";

export type Role = "MANAGER" | "CASHIER" | "BARISTA";
export const ROLES: readonly Role[] = ["MANAGER", "CASHIER", "BARISTA"];
export const ROLE_LABELS: Record<Role, string> = { MANAGER: "Quản lý", CASHIER: "Thu ngân", BARISTA: "Pha chế" };

export interface StaffRow {
  id: string;
  displayName: string;
  loginCode: string;
  enabled: boolean;
  roles: Role[];
}

const isRole = (r: string): r is Role => (ROLES as readonly string[]).includes(r);
const canonical = (roles: readonly Role[]): Role[] => ROLES.filter((r) => roles.includes(r));

export function toStaffRows(data: AuthStaffDetailResponse[] | null | undefined): StaffRow[] {
  return (data ?? []).map((s) => ({
    id: s.id ?? "",
    displayName: s.display_name ?? "",
    loginCode: s.login_code ?? "",
    enabled: s.enabled ?? false,
    roles: canonical((s.roles ?? []).filter(isRole)),
  }));
}

/** Enabled first, then by name; disabled rows only when asked. */
export function filterStaff(rows: StaffRow[], query: string, showDisabled: boolean): StaffRow[] {
  const q = normalizeVietnamese(query);
  return rows
    .filter((r) => showDisabled || r.enabled)
    .filter((r) => !q || normalizeVietnamese(r.displayName).includes(q) || r.loginCode.toLowerCase().includes(q))
    .sort((a, b) => Number(b.enabled) - Number(a.enabled) || a.displayName.localeCompare(b.displayName, "vi"));
}

export interface StaffForm {
  displayName: string;
  loginCode: string;
  roles: Role[];
  pin: string;
  pinConfirm: string;
  enabled: boolean;
}

export type FormMode = "create" | "edit";
export type FormErrors = Partial<Record<"displayName" | "loginCode" | "roles" | "pin" | "pinConfirm", string>>;

export function emptyForm(): StaffForm {
  return { displayName: "", loginCode: "", roles: [], pin: "", pinConfirm: "", enabled: true };
}

export function formFromRow(row: StaffRow): StaffForm {
  return { ...emptyForm(), displayName: row.displayName, loginCode: row.loginCode, roles: [...row.roles], enabled: row.enabled };
}

export function validatePin(pin: string, pinConfirm: string): FormErrors {
  if (!/^\d{4,8}$/.test(pin)) return { pin: "PIN gồm 4–8 chữ số" };
  if (pin !== pinConfirm) return { pinConfirm: "PIN xác nhận không khớp" };
  return {};
}

export function validateForm(form: StaffForm, mode: FormMode): FormErrors {
  const errors: FormErrors = {};
  const name = form.displayName.trim();
  const code = form.loginCode.trim();
  if (!name) errors.displayName = "Vui lòng nhập tên";
  else if (name.length > 120) errors.displayName = "Tên tối đa 120 ký tự";
  if (!code) errors.loginCode = "Vui lòng nhập mã đăng nhập";
  else if (code.length > 24) errors.loginCode = "Mã đăng nhập tối đa 24 ký tự";
  if (form.roles.length === 0) errors.roles = "Chọn ít nhất một vai trò";
  return mode === "create" ? { ...errors, ...validatePin(form.pin, form.pinConfirm) } : errors;
}

export function hasErrors(errors: FormErrors): boolean {
  return Object.keys(errors).length > 0;
}

export function isFormChanged(form: StaffForm, row: StaffRow): boolean {
  return (
    form.displayName.trim() !== row.displayName ||
    form.loginCode.trim().toUpperCase() !== row.loginCode ||
    canonical(form.roles).join() !== canonical(row.roles).join()
  );
}

export function isSelf(row: StaffRow | null, selfId: string | null): boolean {
  return !!row && !!selfId && row.id === selfId;
}

export function toggleRole(roles: Role[], role: Role): Role[] {
  return roles.includes(role) ? roles.filter((r) => r !== role) : canonical([...roles, role]);
}
