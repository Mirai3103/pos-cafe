import { useState, type ReactElement } from "react";
import { AlertTriangle } from "lucide-react";
import { cn, formatVND } from "@/lib/utils";
import { SizeChip, StockSwitch } from "./availability-toggle";
import { StockIconGlyph } from "./stock-icon";
import { blockedMessage, type AvailabilityKind } from "../lib/availability";
import type { StockCard as StockCardData } from "../lib/availability-cards";

export interface StockCardProps {
  card: StockCardData;
  onToggle: (kind: AvailabilityKind, id: string, next: boolean, name: string) => void;
}

function Thumbnail({ card }: { card: StockCardData }): ReactElement {
  const [failed, setFailed] = useState(false);
  return (
    <div className="relative flex h-12 w-12 shrink-0 items-center justify-center overflow-hidden rounded-xl border border-slate-200 bg-slate-100 text-slate-600 dark:border-border dark:bg-muted">
      {card.imageUrl && !failed ? (
        <img src={card.imageUrl} alt={card.name} loading="lazy" className="h-full w-full object-cover" onError={() => setFailed(true)} />
      ) : (
        <StockIconGlyph icon={card.icon} className="h-6 w-6 stroke-[1.5]" />
      )}
    </div>
  );
}

function StatusBadge({ available }: { available: boolean }): ReactElement {
  return available ? (
    <span className="flex items-center gap-1 rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-[11px] font-bold text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-400">
      <span className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
      <span>Còn hàng</span>
    </span>
  ) : (
    <span className="flex items-center gap-1 rounded-full border border-rose-200 bg-rose-100 px-2.5 py-1 text-[11px] font-bold text-rose-700 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-400">
      <span className="h-1.5 w-1.5 rounded-full bg-rose-600" />
      <span>Tạm hết</span>
    </span>
  );
}

/** One card of the "Kho & Món Tạm Hết" grid (design: settings.html renderStockGrid). */
export function StockCard({ card, onToggle }: StockCardProps): ReactElement {
  const warning = card.available ? blockedMessage(card.blockedBy) : null;
  const detail = card.detail ? `${card.groupName} (${card.detail})` : card.groupName;
  return (
    <div
      className={cn(
        "flex flex-col gap-3 rounded-2xl border p-4 shadow-card transition-all duration-200 select-none hover:shadow-card-hover",
        card.available
          ? "border-slate-200 bg-card hover:border-slate-300 dark:border-border"
          : "border-rose-300 bg-rose-50/40 dark:border-rose-800 dark:bg-rose-950/20",
      )}
    >
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <Thumbnail card={card} />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <h4 className="truncate text-sm font-bold leading-snug text-slate-900 dark:text-foreground">{card.name}</h4>
              <span className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-slate-400 dark:bg-muted">
                {card.code}
              </span>
            </div>
            <div className="mt-1 flex items-center gap-2">
              <span className="font-mono text-xs font-bold text-slate-900 dark:text-foreground">
                {card.priceVnd === null ? "—" : formatVND(card.priceVnd)}
              </span>
              <span className="text-slate-300">·</span>
              <span className="truncate text-[11px] text-slate-500 dark:text-muted-foreground">{detail}</span>
            </div>
            <div className="mt-1.5">
              <StatusBadge available={card.available} />
            </div>
          </div>
        </div>
        <StockSwitch checked={card.available} label={card.name} onChange={(next) => onToggle(card.kind, card.id, next, card.name)} />
      </div>

      {card.sizes.length > 0 && (
        <div className="flex flex-wrap gap-2 border-t border-slate-100 pt-3 dark:border-border">
          {card.sizes.map((size) => (
            <SizeChip
              key={size.id}
              checked={size.available}
              label={size.name}
              muted={!card.available}
              onChange={(next) => onToggle("size", size.id, next, `${card.name} (${size.name})`)}
            />
          ))}
        </div>
      )}

      {warning && (
        <p className="flex items-center gap-1.5 rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs font-semibold text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-400">
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
          <span>{warning}</span>
        </p>
      )}
    </div>
  );
}
