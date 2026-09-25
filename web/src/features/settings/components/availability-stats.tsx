import type { ReactElement, ReactNode } from "react";
import { AlertOctagon, CheckCircle2, Coffee, RefreshCw } from "lucide-react";
import { cn } from "@/lib/utils";
import type { StockSummary } from "../lib/availability-cards";

interface StatCardProps {
  label: string;
  value: number;
  hint: string;
  icon: ReactNode;
  tone: "neutral" | "good" | "bad";
}

const TONES = {
  neutral: {
    card: "border-slate-200 dark:border-border",
    label: "text-slate-400",
    value: "text-slate-900 dark:text-foreground",
    hint: "text-slate-500 dark:text-muted-foreground",
    icon: "border-slate-200 bg-slate-100 text-slate-600 dark:border-border dark:bg-muted",
  },
  good: {
    card: "border-emerald-200 dark:border-emerald-900",
    label: "text-emerald-700 dark:text-emerald-400",
    value: "text-emerald-600",
    hint: "font-medium text-emerald-700 dark:text-emerald-400",
    icon: "border-emerald-200 bg-emerald-50 text-emerald-600 dark:border-emerald-900 dark:bg-emerald-950/40",
  },
  bad: {
    card: "border-rose-200 dark:border-rose-900",
    label: "text-rose-700 dark:text-rose-400",
    value: "text-rose-600",
    hint: "font-medium text-rose-600 dark:text-rose-400",
    icon: "border-rose-200 bg-rose-50 text-rose-600 dark:border-rose-900 dark:bg-rose-950/40",
  },
} as const;

function StatCard({ label, value, hint, icon, tone }: StatCardProps): ReactElement {
  const t = TONES[tone];
  return (
    <div className={cn("flex items-center justify-between rounded-2xl border bg-card p-4 shadow-card", t.card)}>
      <div>
        <span className={cn("text-[11px] font-bold tracking-wider uppercase", t.label)}>{label}</span>
        <div className={cn("mt-0.5 font-mono text-2xl font-bold", t.value)}>{value}</div>
        <span className={cn("text-xs", t.hint)}>{hint}</span>
      </div>
      <div className={cn("flex h-10 w-10 items-center justify-center rounded-xl border", t.icon)}>{icon}</div>
    </div>
  );
}

export interface AvailabilityStatsProps {
  summary: StockSummary;
  /** Entries restore-all would turn back on: items, Sizes, and toppings. */
  restoreCount: number;
  onRestore: () => void;
}

export function AvailabilityStats({ summary, restoreCount, onRestore }: AvailabilityStatsProps): ReactElement {
  return (
    <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-2 lg:grid-cols-4">
      <StatCard
        label="Tổng danh mục"
        value={summary.total}
        hint={`${summary.items} món + ${summary.toppings} topping`}
        icon={<Coffee className="h-5 w-5" />}
        tone="neutral"
      />
      <StatCard
        label="Đang còn hàng"
        value={summary.available}
        hint="Bình thường tại quầy"
        icon={<CheckCircle2 className="h-5 w-5" />}
        tone="good"
      />
      <StatCard
        label="Tạm hết hàng"
        value={summary.unavailable}
        hint="Khóa chọn trên POS"
        icon={<AlertOctagon className="h-5 w-5" />}
        tone="bad"
      />
      <div className="flex flex-col justify-center gap-2 rounded-2xl border border-slate-200 bg-card p-3.5 shadow-card dark:border-border">
        <span className="text-[11px] font-bold tracking-wider text-slate-400 uppercase">Thao tác kho nhanh</span>
        <button
          type="button"
          onClick={onRestore}
          disabled={restoreCount === 0}
          className="flex h-12 min-h-[48px] cursor-pointer items-center justify-center gap-2 rounded-xl border border-emerald-300 bg-emerald-50 px-4 text-xs font-bold text-emerald-800 transition select-none hover:bg-emerald-100 active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50 sm:text-sm dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-400"
        >
          <RefreshCw className="h-4 w-4 text-emerald-700 dark:text-emerald-400" />
          <span>{restoreCount > 0 ? `Khôi phục tất cả Còn hàng (${restoreCount})` : "Khôi phục tất cả Còn hàng"}</span>
        </button>
      </div>
    </div>
  );
}
