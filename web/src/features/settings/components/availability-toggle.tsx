import type { ReactElement } from "react";
import { Check, X } from "lucide-react";
import { cn } from "@/lib/utils";

export interface StockSwitchProps {
  checked: boolean;
  /** Entity name, for the accessible label and the hint. */
  label: string;
  onChange: (next: boolean) => void;
}

/** The design's large one-tap switch, with its ON / OFF caption. */
export function StockSwitch({ checked, label, onChange }: StockSwitchProps): ReactElement {
  return (
    <div className="flex shrink-0 flex-col items-center justify-center">
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={label}
        title={checked ? "Nhấn để chuyển sang Tạm hết" : "Nhấn để bật lại Còn hàng"}
        onClick={() => onChange(!checked)}
        className="group flex h-12 min-h-[48px] min-w-[64px] cursor-pointer items-center justify-center rounded-xl focus:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/20"
      >
        <span
          className={cn(
            "flex h-8 w-14 items-center rounded-full p-1 transition-colors duration-200",
            checked ? "bg-emerald-600" : "bg-slate-300 dark:bg-slate-600",
          )}
        >
          <span
            className={cn(
              "flex h-6 w-6 items-center justify-center rounded-full bg-white shadow-md transition-transform duration-200",
              checked ? "translate-x-6" : "translate-x-0",
            )}
          >
            {checked ? (
              <Check className="h-3.5 w-3.5 text-emerald-700" />
            ) : (
              <X className="h-3.5 w-3.5 text-slate-400" />
            )}
          </span>
        </span>
      </button>
      <span className={cn("font-mono text-[10px] font-semibold", checked ? "text-emerald-700" : "text-rose-700")}>
        {checked ? "ON" : "OFF"}
      </span>
    </div>
  );
}

export interface SizeChipProps {
  checked: boolean;
  label: string;
  /** Dimmed but operable, e.g. a Size whose item is off. */
  muted?: boolean;
  onChange: (next: boolean) => void;
}

/** One Size switch inside an item card. */
export function SizeChip({ checked, label, muted = false, onChange }: SizeChipProps): ReactElement {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={cn(
        "flex h-12 min-h-[48px] items-center gap-1.5 rounded-xl border px-3 text-xs font-bold transition select-none active:scale-[0.98]",
        checked
          ? "border-emerald-200 bg-emerald-50 text-emerald-700 hover:bg-emerald-100 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-400"
          : "border-rose-200 bg-rose-100 text-rose-700 hover:bg-rose-200/60 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-400",
        muted && "opacity-50",
      )}
    >
      <span className={cn("h-1.5 w-1.5 rounded-full", checked ? "bg-emerald-500" : "bg-rose-600")} />
      <span>{label}</span>
    </button>
  );
}
