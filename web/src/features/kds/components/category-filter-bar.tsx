import type { ReactElement } from "react";
import { Coffee, Layers } from "lucide-react";
import { cn } from "@/lib/utils";

export interface CategoryFilterBarProps {
  categories: string[];
  active: string | null;
  onSelect: (category: string | null) => void;
}

export function CategoryFilterBar({ categories, active, onSelect }: CategoryFilterBarProps): ReactElement | null {
  if (categories.length === 0) return null;

  return (
    <div className="flex items-center gap-2 bg-slate-100/90 p-1.5 rounded-2xl border border-slate-200/80 overflow-x-auto shrink-0">
      <button
        type="button"
        aria-pressed={active === null}
        onClick={() => onSelect(null)}
        className={cn(
          "min-h-[40px] h-10 px-4 rounded-xl text-xs font-semibold transition flex items-center gap-2 cursor-pointer shrink-0",
          active === null
            ? "bg-white text-slate-900 border border-slate-200 shadow-xs font-bold"
            : "text-slate-600 hover:text-slate-900 hover:bg-white/60",
        )}
      >
        <Layers className="w-4 h-4 text-emerald-600" />
        <span>Tất cả</span>
      </button>
      {categories.map((category) => (
        <button
          key={category}
          type="button"
          aria-pressed={active === category}
          onClick={() => onSelect(category)}
          className={cn(
            "min-h-[40px] h-10 px-4 rounded-xl text-xs font-semibold transition flex items-center gap-2 cursor-pointer shrink-0",
            active === category
              ? "bg-white text-slate-900 border border-slate-200 shadow-xs font-bold"
              : "text-slate-600 hover:text-slate-900 hover:bg-white/60",
          )}
        >
          <Coffee className="w-4 h-4 text-slate-500" />
          <span>{category}</span>
        </button>
      ))}
    </div>
  );
}
