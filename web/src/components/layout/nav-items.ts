import { ChefHat, Clock, Grid2X2, Receipt, Settings, ShoppingCart, type LucideIcon } from "lucide-react";
import { SETTINGS_CAPABILITIES } from "@/features/settings/lib/tabs";

export interface NavItem {
  to: "/" | "/tables" | "/kds" | "/shift" | "/history" | "/settings";
  label: string;
  icon: LucideIcon;
  /** Shown when the session holds any of these; absent means always shown. */
  capabilities?: readonly string[];
}

export const NAV_ITEMS: readonly NavItem[] = [
  { to: "/", label: "Bán hàng", icon: ShoppingCart, capabilities: ["sales.operate"] },
  { to: "/tables", label: "Sơ đồ bàn", icon: Grid2X2, capabilities: ["sales.operate"] },
  { to: "/kds", label: "Bếp KDS", icon: ChefHat, capabilities: ["preparation.operate"] },
  { to: "/shift", label: "Ca làm việc", icon: Clock, capabilities: ["sales_shift.operate"] },
  { to: "/history", label: "Lịch sử", icon: Receipt },
  { to: "/settings", label: "Cài đặt", icon: Settings, capabilities: SETTINGS_CAPABILITIES },
];

export function visibleNavItems(held: readonly string[]): NavItem[] {
  return NAV_ITEMS.filter((item) => !item.capabilities || item.capabilities.some((c) => held.includes(c)));
}
