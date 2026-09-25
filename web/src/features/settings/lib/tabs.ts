import { PackageX, Printer, Store, Utensils, type LucideIcon } from "lucide-react";

export interface SettingsTab {
  to: "/settings/availability";
  label: string;
  icon: LucideIcon;
  capability: string;
}

/** Tabs that exist and route somewhere. The single source for guards and the tab bar. */
export const SETTINGS_TABS: readonly SettingsTab[] = [
  {
    to: "/settings/availability",
    label: "Kho & Món Tạm Hết",
    icon: PackageX,
    capability: "catalog.manage_availability",
  },
];

export const SETTINGS_CAPABILITIES: readonly string[] = SETTINGS_TABS.map((t) => t.capability);

export function permittedTabs(held: readonly string[]): SettingsTab[] {
  return SETTINGS_TABS.filter((t) => held.includes(t.capability));
}

export function firstPermittedTab(held: readonly string[]): SettingsTab | null {
  return permittedTabs(held)[0] ?? null;
}

/**
 * Tabs the design shows that later slices build (9b catalog, then store and
 * print settings, which have no endpoint yet). They render disabled so the bar
 * matches the design; they never route and never grant access. Each is shown
 * only to a session that will be able to open it.
 */
export interface PlannedSettingsTab {
  key: "catalog" | "store" | "system";
  label: string;
  icon: LucideIcon;
  capability: string;
}

export const PLANNED_SETTINGS_TABS: readonly PlannedSettingsTab[] = [
  { key: "catalog", label: "Quản lý Thực đơn & Topping", icon: Utensils, capability: "catalog.administer_structure" },
  { key: "store", label: "Thông tin Quán & VietQR", icon: Store, capability: "staff.administer" },
  { key: "system", label: "Tùy chỉnh In & Hệ thống", icon: Printer, capability: "staff.administer" },
];

export function visiblePlannedTabs(held: readonly string[]): PlannedSettingsTab[] {
  return PLANNED_SETTINGS_TABS.filter((t) => held.includes(t.capability));
}

/** Shown on a planned tab's tooltip. */
export const PLANNED_TAB_HINT = "Sắp ra mắt trong bản cập nhật tới";
