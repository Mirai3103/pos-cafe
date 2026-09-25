import type { ReactElement } from "react";
import { Filter, Inbox, LayoutGrid, RotateCcw, Search, Sparkles } from "lucide-react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { AvailabilityItemCard } from "./availability-item-card";
import { AvailabilityStats } from "./availability-stats";
import { AvailabilityToppings } from "./availability-toppings";
import {
  ALL_SCOPE,
  EMPTY_FILTER,
  TOPPINGS_SCOPE,
  filterGroups,
  filterItems,
  type AvailabilityFilter,
  type AvailabilityKind,
  type AvailabilityView,
} from "../lib/availability";

export interface AvailabilityBoardProps {
  view: AvailabilityView;
  filter: AvailabilityFilter;
  onFilterChange: (filter: AvailabilityFilter) => void;
  onToggle: (kind: AvailabilityKind, id: string, next: boolean) => void;
  onRestore: () => void;
}

function Pill({ active, label, icon: Icon, onClick }: { active: boolean; label: string; icon?: typeof LayoutGrid; onClick: () => void }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "flex min-h-[48px] items-center gap-2 rounded-xl border px-4 text-xs font-bold transition",
        active ? "border-primary bg-primary text-primary-foreground" : "border-border bg-card text-muted-foreground hover:bg-muted",
      )}
    >
      {Icon && <Icon className="h-4 w-4" />}
      <span>{label}</span>
    </button>
  );
}

export function AvailabilityBoard({ view, filter, onFilterChange, onToggle, onRestore }: AvailabilityBoardProps): ReactElement {
  if (view.items.length === 0 && view.groups.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 p-12 text-center text-muted-foreground">
        <Inbox className="h-8 w-8" />
        <p className="text-sm font-semibold">Chưa có món nào trong thực đơn</p>
      </div>
    );
  }

  const items = filterItems(view.items, filter);
  const groups = filterGroups(view.groups, filter);
  const set = (patch: Partial<AvailabilityFilter>) => onFilterChange({ ...filter, ...patch });

  return (
    <div className="flex flex-col gap-4 p-4">
      <AvailabilityStats stats={view.stats} onRestore={onRestore} />

      <div className="flex flex-col gap-3 sm:flex-row">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={filter.query}
            onChange={(e) => set({ query: e.target.value })}
            placeholder="Tìm theo tên món, kích cỡ, topping hoặc danh mục"
            className="h-12 rounded-xl pl-9 text-sm"
          />
        </div>
        <button
          type="button"
          aria-pressed={filter.onlyUnavailable}
          onClick={() => set({ onlyUnavailable: !filter.onlyUnavailable })}
          className={cn(
            "flex min-h-[48px] items-center gap-2 rounded-xl border px-4 text-xs font-bold transition",
            filter.onlyUnavailable ? "border-rose-300 bg-rose-50 text-rose-700" : "border-border bg-card text-muted-foreground hover:bg-muted",
          )}
        >
          <Filter className="h-4 w-4" />
          <span>Chỉ xem món tạm hết</span>
        </button>
      </div>

      <div role="tablist" aria-label="Danh mục" className="flex flex-wrap gap-2">
        <Pill active={filter.scope === ALL_SCOPE} label="Tất cả" icon={LayoutGrid} onClick={() => set({ scope: ALL_SCOPE })} />
        {view.categories.map((cat) => (
          <Pill key={cat.id} active={filter.scope === cat.id} label={cat.name} onClick={() => set({ scope: cat.id })} />
        ))}
        {view.groups.length > 0 && (
          <Pill active={filter.scope === TOPPINGS_SCOPE} label="Topping" icon={Sparkles} onClick={() => set({ scope: TOPPINGS_SCOPE })} />
        )}
      </div>

      {items.length === 0 && groups.length === 0 ? (
        <div className="flex flex-col items-center justify-center gap-3 p-12 text-center text-muted-foreground">
          <Inbox className="h-8 w-8" />
          <p className="text-sm font-semibold">Không tìm thấy món hoặc topping phù hợp</p>
          <button
            type="button"
            onClick={() => onFilterChange(EMPTY_FILTER)}
            className="flex min-h-[48px] items-center gap-2 rounded-xl border border-border bg-card px-4 text-xs font-bold text-foreground hover:bg-muted"
          >
            <RotateCcw className="h-4 w-4" />
            <span>Xóa bộ lọc</span>
          </button>
        </div>
      ) : (
        <>
          {items.length > 0 && (
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
              {items.map((item) => (
                <AvailabilityItemCard key={item.id} item={item} onToggle={onToggle} />
              ))}
            </div>
          )}
          <AvailabilityToppings groups={groups} onToggle={onToggle} />
        </>
      )}
    </div>
  );
}
