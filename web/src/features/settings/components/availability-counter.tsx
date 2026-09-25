import type { ReactElement } from "react";
import { cn } from "@/lib/utils";
import { useAvailabilityMenu } from "../api/use-availability";
import { toAvailabilityView } from "../lib/availability";
import { summarizeCards, toStockCards } from "../lib/availability-cards";

/** The stock tab's badge: emerald at zero, rose while anything is off (design). */
export function TabCounter({ count }: { count: number }): ReactElement {
  return (
    <span
      className={cn(
        "rounded-full border px-2 py-0.5 font-mono text-[11px] font-bold text-white",
        count > 0 ? "border-rose-500 bg-rose-600 shadow-xs" : "border-emerald-500/80 bg-emerald-600/80",
      )}
    >
      {`${count} tạm hết`}
    </span>
  );
}

/** The catalog tab's badge: how many Menu Items exist. */
export function ItemCountBadge({ count }: { count: number }): ReactElement {
  return (
    <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 font-mono text-[11px] font-bold text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-400">
      {`${count} món`}
    </span>
  );
}

function useStockSummary() {
  const { data } = useAvailabilityMenu();
  return summarizeCards(toStockCards(toAvailabilityView(data)));
}

/** Shares the tab's query cache, so it costs no extra request. */
export function AvailabilityCounter(): ReactElement {
  return <TabCounter count={useStockSummary().unavailable} />;
}

export function CatalogItemCounter(): ReactElement {
  return <ItemCountBadge count={useStockSummary().items} />;
}
