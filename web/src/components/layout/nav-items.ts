import { ChefHat, Clock, Grid2X2, LayoutDashboard, Receipt, Settings, ShoppingCart, type LucideIcon } from "lucide-react";
import { MANAGE_CAPABILITIES } from "@/features/manage/lib/tabs";

export interface NavItem {
  to: "/" | "/tables" | "/kds" | "/shift" | "/history" | "/manage" | "/settings";
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
  { to: "/manage", label: "Quản lý", icon: LayoutDashboard, capabilities: MANAGE_CAPABILITIES },
  { to: "/settings", label: "Cài đặt", icon: Settings },
];

export function visibleNavItems(held: readonly string[]): NavItem[] {
  return NAV_ITEMS.filter((item) => !item.capabilities || item.capabilities.some((c) => held.includes(c)));
}
