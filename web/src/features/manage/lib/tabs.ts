import { PackageX, Store, Users, Utensils, type LucideIcon } from "lucide-react";

export interface ManageTab {
  to: "/manage/availability" | "/manage/catalog" | "/manage/staff";
  label: string;
  icon: LucideIcon;
  capability: string;
}

/** Tabs that exist and route somewhere. The single source for guards and the tab bar. */
export const MANAGE_TABS: readonly ManageTab[] = [
  {
    to: "/manage/availability",
    label: "Kho & Món Tạm Hết",
    icon: PackageX,
    capability: "catalog.manage_availability",
  },
  {
    to: "/manage/catalog",
    label: "Quản lý Thực đơn & Topping",
    icon: Utensils,
    capability: "catalog.administer_structure",
  },
  {
    to: "/manage/staff",
    label: "Nhân viên",
    icon: Users,
    capability: "staff.administer",
  },
];

export const MANAGE_CAPABILITIES: readonly string[] = MANAGE_TABS.map((t) => t.capability);

export function permittedTabs(held: readonly string[]): ManageTab[] {
  return MANAGE_TABS.filter((t) => held.includes(t.capability));
}

export function firstPermittedTab(held: readonly string[]): ManageTab | null {
  return permittedTabs(held)[0] ?? null;
}

/**
 * Tabs the design shows that later work builds (the store profile, which has
 * no endpoint yet, BA-2). They render disabled so the bar matches the
 * design; they never route and never grant access. Each is shown only to a
 * session that will be able to open it.
 */
export interface PlannedManageTab {
  key: "store";
  label: string;
  icon: LucideIcon;
  capability: string;
}

export const PLANNED_MANAGE_TABS: readonly PlannedManageTab[] = [
  { key: "store", label: "Thông tin Quán & VietQR", icon: Store, capability: "staff.administer" },
];

export function visiblePlannedTabs(held: readonly string[]): PlannedManageTab[] {
  return PLANNED_MANAGE_TABS.filter((t) => held.includes(t.capability));
}

/** Shown on a planned tab's tooltip. */
export const PLANNED_TAB_HINT = "Sắp ra mắt trong bản cập nhật tới";
