import type { ReactElement } from "react";
import { Filter, Inbox, LayoutGrid, RotateCcw, Search, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { StockCard } from "./availability-item-card";
import { AvailabilityStats } from "./availability-stats";
import { StockIconGlyph } from "./stock-icon";
import {
  ALL_SCOPE,
  EMPTY_FILTER,
  TOPPINGS_SCOPE,
  type AvailabilityFilter,
  type AvailabilityKind,
  type AvailabilityView,
} from "../lib/availability";
import { categoryIcon, filterCards, summarizeCards, toStockCards, type StockIcon } from "../lib/availability-cards";

export interface AvailabilityBoardProps {
  view: AvailabilityView;
  filter: AvailabilityFilter;
  onFilterChange: (filter: AvailabilityFilter) => void;
  onToggle: (kind: AvailabilityKind, id: string, next: boolean, name: string) => void;
  onRestore: () => void;
}

function Pill({ active, label, icon, onClick }: { active: boolean; label: string; icon?: StockIcon | "all"; onClick: () => void }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "flex h-12 min-h-[48px] shrink-0 cursor-pointer items-center gap-2 rounded-xl px-4 text-xs transition select-none sm:text-sm",
        active
          ? "bg-slate-900 font-bold text-white shadow-xs dark:bg-foreground dark:text-background"
          : "border border-slate-200 font-medium text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:border-border dark:text-muted-foreground dark:hover:bg-muted",
      )}
    >
      {icon === "all" ? <LayoutGrid className="h-4 w-4" /> : icon ? <StockIconGlyph icon={icon} className="h-4 w-4" /> : null}
      <span>{label}</span>
    </button>
  );
}

function EmptyPanel({ title, hint, action }: { title: string; hint: string; action?: ReactElement }): ReactElement {
  return (
    <div className="col-span-full flex flex-col items-center justify-center rounded-2xl border border-slate-200 bg-card p-8 py-16 text-center text-slate-400 dark:border-border">
      <div className="mb-3 flex h-12 w-12 items-center justify-center rounded-2xl bg-slate-100 text-slate-400 dark:bg-muted">
        <Inbox className="h-6 w-6 stroke-[1.5]" />
      </div>
      <p className="text-sm font-bold text-slate-800 dark:text-foreground">{title}</p>
      <p className="mt-1 max-w-sm text-xs text-slate-400">{hint}</p>
      {action}
    </div>
  );
}

export function AvailabilityBoard({ view, filter, onFilterChange, onToggle, onRestore }: AvailabilityBoardProps): ReactElement {
  const allCards = toStockCards(view);
  const summary = summarizeCards(allCards);
  const set = (patch: Partial<AvailabilityFilter>) => onFilterChange({ ...filter, ...patch });

  if (allCards.length === 0) {
    return (
      <section className="space-y-5">
        <AvailabilityStats summary={summary} restoreCount={view.unavailableRefs.length} onRestore={onRestore} />
        <EmptyPanel title="Chưa có món nào trong thực đơn" hint="Thêm món ở tab Quản lý Thực đơn & Topping để bật/tắt tại đây." />
      </section>
    );
  }

  const cards = filterCards(allCards, filter);

  return (
    <section className="space-y-5">
      <AvailabilityStats summary={summary} restoreCount={view.unavailableRefs.length} onRestore={onRestore} />

      <div className="space-y-3 rounded-2xl border border-slate-200 bg-card p-4 shadow-card dark:border-border">
        <div className="flex flex-col items-stretch gap-3 md:flex-row md:items-center">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute top-1/2 left-3.5 h-4 w-4 -translate-y-1/2 text-slate-400" />
            <input
              type="text"
              value={filter.query}
              onChange={(e) => set({ query: e.target.value })}
              aria-label="Tìm món hoặc topping"
              placeholder="Tìm theo tên món hoặc mã (cfsd, bạc xỉu, trà đào, topping...)"
              className="h-12 min-h-[48px] w-full rounded-xl border border-slate-200 bg-slate-50 pr-10 pl-10 text-sm font-medium text-slate-900 transition outline-none placeholder:text-slate-400 focus:border-emerald-500 focus:bg-white focus:ring-2 focus:ring-emerald-500/20 dark:border-border dark:bg-muted dark:text-foreground"
            />
            {filter.query && (
              <button
                type="button"
                aria-label="Xóa tìm kiếm"
                onClick={() => set({ query: "" })}
                className="absolute inset-y-0 right-0 flex h-12 min-h-[48px] w-12 min-w-[48px] cursor-pointer items-center justify-center text-slate-400 hover:text-slate-600"
              >
                <X className="h-4 w-4" />
              </button>
            )}
          </div>
          <button
            type="button"
            aria-pressed={filter.onlyUnavailable}
            onClick={() => set({ onlyUnavailable: !filter.onlyUnavailable })}
            className={cn(
              "flex h-12 min-h-[48px] cursor-pointer items-center justify-center gap-2 rounded-xl px-4 text-xs transition select-none sm:text-sm",
              filter.onlyUnavailable
                ? "border-2 border-rose-500 bg-rose-50 font-bold text-rose-800 shadow-xs dark:bg-rose-950/40 dark:text-rose-300"
                : "border border-slate-200 bg-card font-semibold text-slate-700 shadow-2xs hover:bg-slate-50 dark:border-border dark:text-foreground dark:hover:bg-muted",
            )}
          >
            <Filter className={cn("h-4 w-4", filter.onlyUnavailable ? "text-rose-500" : "text-slate-400")} />
            <span>{filter.onlyUnavailable ? "Đang lọc: Chỉ món Tạm hết" : "Chỉ xem món Tạm hết"}</span>
          </button>
        </div>

        <div role="tablist" aria-label="Danh mục" className="flex items-center gap-2 overflow-x-auto pt-1 pb-1">
          <Pill active={filter.scope === ALL_SCOPE} label="Tất cả" icon="all" onClick={() => set({ scope: ALL_SCOPE })} />
          {view.categories.map((cat) => (
            <Pill key={cat.id} active={filter.scope === cat.id} label={cat.name} icon={categoryIcon(cat.name)} onClick={() => set({ scope: cat.id })} />
          ))}
          {view.groups.length > 0 && (
            <Pill active={filter.scope === TOPPINGS_SCOPE} label="Topping" icon="layers" onClick={() => set({ scope: TOPPINGS_SCOPE })} />
          )}
        </div>
      </div>

      <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2 xl:grid-cols-3">
        {cards.length === 0 ? (
          <EmptyPanel
            title="Không tìm thấy món hoặc topping phù hợp"
            hint="Thử thay đổi từ khóa tìm kiếm hoặc chọn danh mục khác."
            action={
              <button
                type="button"
                onClick={() => onFilterChange(EMPTY_FILTER)}
                className="mt-4 flex h-12 min-h-[48px] cursor-pointer items-center gap-1.5 rounded-xl border border-emerald-300 bg-emerald-50 px-5 text-xs font-bold text-emerald-800 transition select-none hover:bg-emerald-100 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-400"
              >
                <RotateCcw className="h-4 w-4" />
                <span>Xóa bộ lọc & Hiển thị tất cả</span>
              </button>
            }
          />
        ) : (
          cards.map((card) => <StockCard key={`${card.kind}:${card.id}`} card={card} onToggle={onToggle} />)
        )}
      </div>
    </section>
  );
}
