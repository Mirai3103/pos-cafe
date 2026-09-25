import { PackageX, type LucideIcon } from "lucide-react";

export interface SettingsTab {
  to: "/settings/availability";
  label: string;
  icon: LucideIcon;
  capability: string;
}

/** Tabs that exist. A tab not yet built is absent, never a placeholder. */
export const SETTINGS_TABS: readonly SettingsTab[] = [
  {
    to: "/settings/availability",
    label: "Món tạm hết",
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
