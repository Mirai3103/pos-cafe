// web/src/features/catalog/components/item-card.tsx
import type { ReactElement } from "react";
import { Coffee, Pencil } from "lucide-react";
import { BADGE_LABELS, displayCode, priceLabel, type CatItem } from "../lib/catalog-model";

export interface ItemCardProps {
  item: CatItem;
  categoryName: string;
  groupCount: number;
  onOpen: () => void;
}

export function ItemCard({ item, categoryName, groupCount, onOpen }: ItemCardProps): ReactElement {
  const facts = [categoryName, item.sizes.length > 0 ? `${item.sizes.length} kích cỡ` : null, `${groupCount} nhóm topping`]
    .filter(Boolean)
    .join(" · ");
  return (
    <button
      type="button"
      onClick={onOpen}
      className="group flex w-full gap-3.5 rounded-2xl border border-slate-200 bg-card p-3.5 text-left shadow-2xs transition hover:border-emerald-300 hover:shadow-sm active:scale-[0.99] dark:border-border"
    >
      <div className="flex h-20 w-20 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-slate-100 dark:bg-muted">
        {item.imageUrl ? (
          <img src={item.imageUrl} alt="" loading="lazy" className="h-full w-full object-cover" />
        ) : (
          <Coffee className="h-7 w-7 text-slate-400" />
        )}
      </div>
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex items-center gap-2">
          <span className="rounded-md bg-slate-100 px-1.5 py-0.5 font-mono text-3xs font-bold text-slate-600 uppercase dark:bg-muted dark:text-muted-foreground">
            {displayCode(item)}
          </span>
          {item.badge && (
            <span className="rounded-md bg-amber-100 px-1.5 py-0.5 text-3xs font-bold text-amber-800 dark:bg-amber-950/40 dark:text-amber-300">
              {BADGE_LABELS[item.badge]}
            </span>
          )}
        </div>
        <p className="truncate text-sm font-bold text-slate-900 dark:text-foreground">{item.name}</p>
        <p className="truncate text-2xs text-slate-500 dark:text-muted-foreground">{facts}</p>
        <p className="font-mono text-sm font-bold text-emerald-700 dark:text-emerald-400">{priceLabel(item)}</p>
      </div>
      <Pencil className="h-4 w-4 shrink-0 text-slate-300 group-hover:text-emerald-600" />
    </button>
  );
}
