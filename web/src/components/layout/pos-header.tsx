import * as React from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import {
  Coffee,
  ShoppingCart,
  Grid2X2,
  ChefHat,
  Clock,
  Receipt,
  Settings,
  Lock,
  User,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

const navItems = [
  { to: "/", label: "Bán hàng", icon: ShoppingCart },
  { to: "/tables", label: "Sơ đồ bàn", icon: Grid2X2 },
  { to: "/kds", label: "Bếp KDS", icon: ChefHat },
  { to: "/shift", label: "Ca làm việc", icon: Clock },
  { to: "/history", label: "Lịch sử", icon: Receipt },
  { to: "/settings", label: "Cài đặt", icon: Settings },
];

export function PosHeader() {
  const routerState = useRouterState();
  const currentPath = routerState.location.pathname;

  const [time, setTime] = React.useState<string>("");

  React.useEffect(() => {
    const updateTime = () => {
      const now = new Date();
      setTime(
        now.toLocaleTimeString("vi-VN", {
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        })
      );
    };
    updateTime();
    const timer = setInterval(updateTime, 1000);
    return () => clearInterval(timer);
  }, []);

  return (
    <header className="flex h-16 w-full items-center justify-between border-b border-border bg-card px-4 shrink-0 select-none">
      {/* Brand & Acronym */}
      <div className="flex items-center gap-6">
        <Link to="/" className="flex items-center gap-2.5 font-bold text-foreground">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-xs">
            <Coffee className="h-5 w-5" />
          </div>
          <div className="flex flex-col">
            <span className="text-base font-bold tracking-tight">Crisp Emerald</span>
            <span className="text-2xs font-mono text-muted-foreground uppercase">POS Terminal v1.0</span>
          </div>
        </Link>

        {/* Navigation Tabs */}
        <nav className="hidden lg:flex items-center gap-1">
          {navItems.map((item) => {
            const Icon = item.icon;
            const isActive = currentPath === item.to;
            return (
              <Link
                key={item.to}
                to={item.to}
                className={`flex items-center gap-2 rounded-lg px-3.5 py-2 text-sm font-medium transition-colors ${
                  isActive
                    ? "bg-primary text-primary-foreground shadow-xs"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                }`}
              >
                <Icon className="h-4 w-4" />
                <span>{item.label}</span>
              </Link>
            );
          })}
        </nav>
      </div>

      {/* Right Shell Controls */}
      <div className="flex items-center gap-3">
        {/* Real-time Digital Clock */}
        <div className="hidden sm:flex items-center gap-2 rounded-lg bg-muted px-3 py-1.5">
          <Clock className="h-4 w-4 text-muted-foreground" />
          <span className="text-sm font-semibold font-mono tracking-wide text-foreground">{time}</span>
        </div>

        {/* Active Cashier & Shift Badge */}
        <div className="flex items-center gap-2 border-l border-border pl-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10 text-primary">
            <User className="h-4 w-4" />
          </div>
          <div className="hidden md:flex flex-col text-left">
            <span className="text-xs font-semibold text-foreground">Thu ngân #01</span>
            <span className="text-2xs text-muted-foreground">Ca sáng (06:00 - 14:00)</span>
          </div>
          <Badge variant="secondary" className="hidden xl:inline-flex">Đang mở ca</Badge>
        </div>

        {/* Lock Screen / Logout */}
        <Link to="/auth/login">
          <Button variant="outline" size="icon" className="h-9 w-9 rounded-lg" title="Khóa màn hình">
            <Lock className="h-4 w-4 text-muted-foreground" />
          </Button>
        </Link>
      </div>
    </header>
  );
}
