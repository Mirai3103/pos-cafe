import type { ReactElement } from "react";
import { AlertTriangle } from "lucide-react";
import { cn } from "@/lib/utils";
import { AvailabilityToggle } from "./availability-toggle";
import { blockedMessage, type AvailabilityItemView, type AvailabilityKind } from "../lib/availability";

export interface AvailabilityItemCardProps {
  item: AvailabilityItemView;
  onToggle: (kind: AvailabilityKind, id: string, next: boolean) => void;
}

export function AvailabilityItemCard({ item, onToggle }: AvailabilityItemCardProps): ReactElement {
  const warning = item.available ? blockedMessage(item.blockedBy) : null;
  return (
    <div
      className={cn(
        "flex flex-col gap-3 rounded-2xl border p-4",
        item.available ? "border-border bg-card" : "border-rose-300 bg-rose-50/40 dark:border-rose-800 dark:bg-rose-950/20",
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h4 className="truncate text-sm font-bold text-foreground">{item.name}</h4>
          <span className="text-xs text-muted-foreground">{item.categoryName}</span>
        </div>
        <AvailabilityToggle checked={item.available} label={item.name} onChange={(next) => onToggle("item", item.id, next)} />
      </div>
      {item.sizes.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {item.sizes.map((size) => (
            <AvailabilityToggle
              key={size.id}
              size="chip"
              checked={size.available}
              label={size.name}
              muted={!item.available}
              onChange={(next) => onToggle("size", size.id, next)}
            />
          ))}
        </div>
      )}
      {warning && (
        <p className="flex items-center gap-1.5 text-xs font-semibold text-amber-700 dark:text-amber-400">
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
          <span>{warning}</span>
        </p>
      )}
    </div>
  );
}
