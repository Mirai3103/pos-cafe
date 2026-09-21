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
  LogOut,
  Volume2,
  VolumeX,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { useSessionStore } from "@/stores/use-session-store";
import { useLock, useSignOut } from "@/features/auth/api/use-auth";
import { useSound, playTapChirp } from "@/lib/sound";

const navItems = [
  { to: "/", label: "Bán hàng", icon: ShoppingCart },
  { to: "/tables", label: "Sơ đồ bàn", icon: Grid2X2 },
  { to: "/kds", label: "Bếp KDS", icon: ChefHat },
  { to: "/shift", label: "Ca làm việc", icon: Clock },
  { to: "/history", label: "Lịch sử", icon: Receipt },
  { to: "/settings", label: "Cài đặt", icon: Settings },
];

const WORKSPACE_LABELS: Record<string, string> = {
  cashier: "Quầy thu ngân",
  preparation: "Khu pha chế",
  manager: "Quản lý",
};

export function PosHeader() {
  const routerState = useRouterState();
  const currentPath = routerState.location.pathname;
  const displayName = useSessionStore((s) => s.displayName);
  const workspace = useSessionStore((s) => s.workspace);
  const lock = useLock();
  const signOut = useSignOut();
  const { enabled: soundEnabled, toggle: toggleSound } = useSound();

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
        {/* Sound Feedback Toggle */}
        <button
          type="button"
          onClick={toggleSound}
          className={`flex h-10 w-10 min-h-[40px] min-w-[40px] cursor-pointer items-center justify-center rounded-xl border transition select-none active:scale-95 focus:outline-none focus:ring-2 ${
            soundEnabled
              ? "border-emerald-200 bg-emerald-50/70 text-emerald-700 hover:bg-emerald-100/70 focus:ring-emerald-500/20 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-400"
              : "border-border bg-muted text-muted-foreground hover:bg-muted/80 focus:ring-muted"
          }`}
          title={soundEnabled ? "Âm thanh phản hồi: Đang bật (Click để tắt)" : "Âm thanh phản hồi: Đang tắt (Click để bật)"}
          aria-label="Bật/tắt âm thanh"
          aria-pressed={soundEnabled}
        >
          {soundEnabled ? <Volume2 className="size-4" /> : <VolumeX className="size-4" />}
        </button>

        {/* Real-time Digital Clock */}
        <div className="hidden sm:flex items-center gap-2 rounded-lg bg-muted px-3 py-1.5">
          <Clock className="h-4 w-4 text-muted-foreground" />
          <span className="text-sm font-semibold font-mono tracking-wide text-foreground">{time}</span>
        </div>

        {/* Staff identity & session actions */}
        <div className="flex items-center gap-2">
          <div className="text-right">
            <div className="text-sm font-semibold text-foreground">{displayName ?? "—"}</div>
            <div className="text-2xs text-muted-foreground">
              {workspace ? WORKSPACE_LABELS[workspace] : "Chưa chọn khu vực"}
            </div>
          </div>
          <Button
            variant="outline"
            size="icon"
            className="h-12 w-12 rounded-xl"
            aria-label="Khóa màn hình"
            disabled={lock.isPending}
            onClick={() => {
              playTapChirp();
              lock.mutate();
            }}
          >
            <Lock className="size-5" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-12 w-12 rounded-xl"
            aria-label="Đăng xuất"
            disabled={signOut.isPending}
            onClick={() => {
              playTapChirp();
              signOut.mutate();
            }}
          >
            <LogOut className="size-5" />
          </Button>
        </div>
      </div>
    </header>
  );
}
