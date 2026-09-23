import type { ReactElement } from "react";
import { cn } from "@/lib/utils";

export interface CategoryFilterBarProps {
  categories: string[];
  active: string | null;
  onSelect: (category: string | null) => void;
}

export function CategoryFilterBar({ categories, active, onSelect }: CategoryFilterBarProps): ReactElement | null {
  if (categories.length === 0) return null;

  const chipClass = (isActive: boolean) =>
    cn(
      "h-9 shrink-0 rounded-full border px-3.5 text-xs font-semibold transition",
      isActive
        ? "border-primary bg-primary text-primary-foreground"
        : "border-border bg-background text-muted-foreground hover:bg-muted",
    );

  return (
    <div className="flex items-center gap-2 overflow-x-auto pb-1">
      <button type="button" aria-pressed={active === null} onClick={() => onSelect(null)} className={chipClass(active === null)}>
        Tất cả
      </button>
      {categories.map((category) => (
        <button
          key={category}
          type="button"
          aria-pressed={active === category}
          onClick={() => onSelect(category)}
          className={chipClass(active === category)}
        >
          {category}
        </button>
      ))}
    </div>
  );
}
