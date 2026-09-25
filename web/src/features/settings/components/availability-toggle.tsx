import type { ReactElement } from "react";
import { cn } from "@/lib/utils";

export interface AvailabilityToggleProps {
  checked: boolean;
  label: string;
  onChange: (next: boolean) => void;
  size?: "lg" | "chip";
  /** Dimmed but operable, e.g. a Size whose item is off. */
  muted?: boolean;
}

export function AvailabilityToggle({ checked, label, onChange, size = "lg", muted = false }: AvailabilityToggleProps): ReactElement {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={cn(
        "flex min-h-[48px] items-center gap-2 rounded-xl border px-3 text-xs font-bold transition active:scale-95",
        checked
          ? "border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-400"
          : "border-rose-300 bg-rose-50 text-rose-700 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-400",
        size === "lg" && "min-w-[120px] justify-center",
        muted && "opacity-50",
      )}
    >
      <span className={cn("h-2.5 w-2.5 rounded-full", checked ? "bg-emerald-500" : "bg-rose-500")} />
      <span>{size === "lg" ? (checked ? "Còn hàng" : "Tạm hết") : label}</span>
    </button>
  );
}
