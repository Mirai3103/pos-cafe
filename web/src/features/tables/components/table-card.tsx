import {
  AlertCircle,
  ChevronRight,
  MapPin,
  MoreHorizontal,
  Pencil,
  PlusCircle,
  Power,
} from "lucide-react";
import * as React from "react";
import type { FloorTable } from "../lib/floor";

export interface TableCardProps {
  table: FloorTable;
  canAdminister: boolean;
  onOpen: (table: FloorTable) => void;
  onRename: (table: FloorTable) => void;
  onToggleAvailability: (table: FloorTable) => void;
}

export function TableCard({
  table,
  canAdminister,
  onOpen,
  onRename,
  onToggleAvailability,
}: TableCardProps) {
  const [isMenuOpen, setIsMenuOpen] = React.useState(false);
  const switchedOff = !table.available && table.state === "OCCUPIED";

  // 1. Available Table (FREE / Trống)
  if (table.state === "FREE") {
    return (
      <div className="relative rounded-2xl border-2 border-dashed border-slate-200 dark:border-border/80 bg-card p-4 min-h-[175px] flex flex-col justify-between select-none shadow-2xs hover:border-emerald-400 dark:hover:border-emerald-500 hover:bg-emerald-50/10 transition-all active:scale-[0.985]">
        <button
          type="button"
          onClick={() => onOpen(table)}
          className="w-full h-full flex flex-col justify-between text-left cursor-pointer rounded-2xl focus:outline-none"
        >
          <div>
            <div className="flex items-start justify-between gap-1 pr-10">
              <div className="flex items-center gap-2">
                <span className="font-mono font-bold text-base text-foreground">{table.name}</span>
              </div>
              <span className="px-2 py-0.5 rounded-full text-[11px] font-semibold bg-slate-100 text-slate-600 dark:bg-muted dark:text-muted-foreground border border-slate-200 dark:border-border">
                Trống
              </span>
            </div>
            <div className="flex items-center gap-1 text-xs text-muted-foreground mt-1.5">
              <MapPin className="w-3.5 h-3.5 text-muted-foreground/70" />
              <span>Khu vực chính</span>
            </div>
          </div>

          <div className="pt-3 border-t border-slate-100 dark:border-border flex items-center justify-between">
            <span className="min-h-[48px] flex items-center gap-1.5 text-xs font-bold text-emerald-700 dark:text-emerald-400">
              <PlusCircle className="w-4 h-4" />
              <span>+ Mở bàn phục vụ</span>
            </span>
            <span className="text-[11px] font-mono text-muted-foreground">Sẵn sàng</span>
          </div>
        </button>

        {renderAdminMenu()}
      </div>
    );
  }

  // 2. Occupied Table (Có khách / Đang ngồi)
  if (table.state === "OCCUPIED") {
    return (
      <div className="relative rounded-2xl border border-indigo-100 dark:border-indigo-900/50 bg-card p-4 min-h-[175px] flex flex-col justify-between select-none shadow-card hover:shadow-card-hover hover:border-indigo-300 dark:hover:border-indigo-700 transition-all active:scale-[0.985]">
        <button
          type="button"
          onClick={() => onOpen(table)}
          className="w-full h-full flex flex-col justify-between text-left cursor-pointer rounded-2xl focus:outline-none"
        >
          <div>
            <div className="flex items-start justify-between gap-1 pr-10">
              <div className="flex items-center gap-2">
                <span className="font-mono font-bold text-base text-foreground">{table.name}</span>
              </div>
              <div className="flex items-center gap-1">
                {switchedOff && (
                  <span className="px-2 py-0.5 rounded-full text-[11px] font-bold bg-amber-100 text-amber-800 dark:bg-amber-950/60 dark:text-amber-300 border border-amber-300 dark:border-amber-800 flex items-center gap-1">
                    <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-ping" />
                    <span>Tạm ngưng</span>
                  </span>
                )}
                <span className="px-2 py-0.5 rounded-full text-[11px] font-bold bg-indigo-50 text-indigo-700 dark:bg-indigo-950/60 dark:text-indigo-300 border border-indigo-200 dark:border-indigo-800 flex items-center gap-1.5">
                  <span className="w-1.5 h-1.5 rounded-full bg-indigo-600" />
                  <span>Có khách</span>
                </span>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground mt-2">
              {table.occupants.map((o) => (
                <span
                  key={o.sessionId}
                  className="rounded-md bg-muted px-2 py-0.5 font-mono text-xs font-semibold text-foreground"
                >
                  {`#${o.serviceNumber}`}
                </span>
              ))}
              {table.occupants.length > 1 && (
                <span className="px-1.5 py-0.5 rounded bg-indigo-50 dark:bg-indigo-950/60 text-indigo-700 dark:text-indigo-300 text-[10px] font-bold border border-indigo-200 dark:border-indigo-800">
                  {`Ghép ${table.occupants.length} đơn`}
                </span>
              )}
            </div>
          </div>

          <div className="pt-3 border-t border-slate-100 dark:border-border flex items-center justify-between">
            <div className="flex flex-col">
              <span className="text-[10px] text-muted-foreground uppercase font-bold tracking-wider">
                Đơn phục vụ
              </span>
              <span className="font-mono font-bold text-sm text-foreground tabular-nums">
                {table.occupants.map((o) => `#${o.serviceNumber}`).join(", ")}
              </span>
            </div>
            <span className="min-h-[48px] flex items-center text-[11px] font-bold text-indigo-600 dark:text-indigo-400 gap-1">
              <span>Thao tác</span>
              <ChevronRight className="w-4 h-4" />
            </span>
          </div>
        </button>

        {renderAdminMenu()}
      </div>
    );
  }

  // 3. Unavailable Table (UNAVAILABLE / Tạm ngưng)
  return (
    <div className="relative rounded-2xl border-2 border-dashed border-slate-200 dark:border-border/60 bg-muted/20 p-4 min-h-[175px] flex flex-col justify-between select-none opacity-60 transition-all">
      <button
        type="button"
        disabled
        className="w-full h-full flex flex-col justify-between text-left cursor-not-allowed rounded-2xl focus:outline-none"
      >
        <div>
          <div className="flex items-start justify-between gap-1 pr-10">
            <div className="flex items-center gap-2">
              <span className="font-mono font-bold text-base text-muted-foreground">{table.name}</span>
            </div>
            <span className="px-2 py-0.5 rounded-full text-[11px] font-bold bg-slate-100 text-slate-500 dark:bg-muted dark:text-muted-foreground border border-slate-200 dark:border-border flex items-center gap-1">
              <Power className="w-3 h-3 text-slate-400" />
              <span>Tạm ngưng</span>
            </span>
          </div>
          <div className="flex items-center gap-1 text-xs text-muted-foreground mt-1.5">
            <AlertCircle className="w-3.5 h-3.5 text-muted-foreground/70" />
            <span>Ngưng nhận khách</span>
          </div>
        </div>

        <div className="pt-3 border-t border-muted/50 flex items-center justify-between text-xs text-muted-foreground">
          <span className="min-h-[48px] flex items-center">Chưa sẵn sàng</span>
          <span className="text-[11px] font-mono">Đóng</span>
        </div>
      </button>

      {renderAdminMenu()}
    </div>
  );

  function renderAdminMenu() {
    if (!canAdminister) return null;
    return (
      <div className="absolute top-2 right-2">
        <button
          type="button"
          aria-label={`Tùy chọn ${table.name}`}
          title={`Đổi tên / ${table.available ? "Tạm ngưng" : "Mở lại"}`}
          onClick={() => setIsMenuOpen((open) => !open)}
          className="h-12 w-12 min-h-[48px] rounded-xl flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted/80 active:scale-[0.98] transition focus:outline-none focus:ring-2 focus:ring-primary/20"
        >
          <MoreHorizontal className="h-4 w-4" />
        </button>
        {isMenuOpen && (
          <div
            role="menu"
            className="absolute right-0 z-20 mt-1 w-44 rounded-2xl border border-border bg-card p-1.5 shadow-xl"
          >
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                setIsMenuOpen(false);
                onRename(table);
              }}
              className="w-full min-h-[48px] px-3 rounded-xl text-left text-xs font-semibold flex items-center gap-2 hover:bg-muted transition text-foreground"
            >
              <Pencil className="h-4 w-4 text-muted-foreground" />
              <span>Đổi tên</span>
            </button>
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                setIsMenuOpen(false);
                onToggleAvailability(table);
              }}
              className="w-full min-h-[48px] px-3 rounded-xl text-left text-xs font-semibold flex items-center gap-2 hover:bg-muted transition text-foreground"
            >
              <Power className="h-4 w-4 text-muted-foreground" />
              <span>{table.available ? "Tạm ngưng" : "Mở lại"}</span>
            </button>
          </div>
        )}
      </div>
    );
  }
}
