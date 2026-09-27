import type { ReactElement } from "react";
import { Coffee, Layers, LayoutGrid, Plus, Utensils, Zap, type LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import type { CatalogViewKey } from "../lib/views";

export interface CatalogCounts {
  items: number;
  categories: number;
  groups: number;
}

const PILLS: { view: CatalogViewKey; label: string; icon: LucideIcon; count?: (c: CatalogCounts) => string }[] = [
  { view: "items", label: "Món & Định giá", icon: Coffee, count: (c) => `${c.items} món` },
  { view: "categories", label: "Danh mục món", icon: LayoutGrid, count: (c) => `${c.categories} danh mục` },
  { view: "groups", label: "Nhóm Topping", icon: Layers, count: (c) => `${c.groups} nhóm` },
  { view: "linker", label: "Ma trận Gán Topping (Batch Linker)", icon: Zap },
];

export function CatalogHeader({ canAdd, onNewItem }: { canAdd: boolean; onNewItem: () => void }): ReactElement {
  return (
    <div className="flex flex-col justify-between gap-4 rounded-2xl border border-slate-200 bg-card p-5 shadow-2xs sm:flex-row sm:items-center dark:border-border">
      <div className="flex items-center gap-3.5">
        <div className="flex h-12 w-12 items-center justify-center rounded-2xl border border-emerald-200 bg-emerald-50 text-emerald-600 dark:border-emerald-900 dark:bg-emerald-950/40">
          <Utensils className="h-6 w-6" />
        </div>
        <div>
          <h2 className="text-base leading-tight font-bold text-slate-900 sm:text-lg dark:text-foreground">
            Trung tâm Quản lý Thực đơn & Nhóm Topping
          </h2>
          <p className="mt-1 text-xs text-slate-500 dark:text-muted-foreground">
            Thiết lập món, định giá đa kích cỡ (S/M/L), nhóm tùy chọn và ma trận gán topping hàng loạt cho quầy thu ngân.
          </p>
        </div>
      </div>
      <button
        type="button"
        disabled={!canAdd}
        title={canAdd ? undefined : "Tạo danh mục trước"}
        onClick={onNewItem}
        className="flex h-12 min-h-[48px] shrink-0 items-center justify-center gap-2 rounded-xl bg-emerald-600 px-5 text-xs font-bold text-white shadow-sm transition hover:bg-emerald-700 active:scale-[0.98] disabled:opacity-50 sm:text-sm"
      >
        <Plus className="h-4 w-4" />+ THÊM MÓN MỚI (Ctrl+N)
      </button>
    </div>
  );
}

export function CatalogPills({
  view,
  counts,
  onChange,
}: {
  view: CatalogViewKey;
  counts: CatalogCounts;
  onChange: (view: CatalogViewKey) => void;
}): ReactElement {
  return (
    <div className="rounded-2xl border border-slate-200 bg-card p-2 shadow-2xs dark:border-border">
      <div role="tablist" aria-label="Phân hệ thực đơn" className="flex items-center gap-2 overflow-x-auto">
        {PILLS.map(({ view: key, label, icon: Icon, count }) => {
          const active = key === view;
          return (
            <button
              key={key}
              type="button"
              role="tab"
              aria-selected={active}
              onClick={() => onChange(key)}
              className={cn(
                "flex h-12 min-h-[48px] shrink-0 items-center gap-2 rounded-xl px-4 text-xs whitespace-nowrap transition select-none sm:text-sm",
                active
                  ? "bg-slate-900 font-bold text-white shadow-xs dark:bg-foreground dark:text-background"
                  : "font-semibold text-slate-600 hover:bg-slate-100 dark:text-muted-foreground dark:hover:bg-muted",
              )}
            >
              <Icon className={cn("h-4 w-4", active ? "text-white dark:text-background" : "text-slate-400")} />
              <span>{label}</span>
              {count && (
                <span
                  className={cn(
                    "rounded-full px-2 py-0.5 font-mono text-[11px] font-bold",
                    active ? "bg-white/15 text-white" : "bg-slate-100 text-slate-600 dark:bg-muted",
                  )}
                >
                  {count(counts)}
                </span>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
