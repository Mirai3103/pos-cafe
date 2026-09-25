import type { PosPhase } from "../utils/phase";
import {
  PENDING_PHASE_LABELS,
  formatAge,
  minutesSince,
  type PendingOrder,
} from "../utils/pending-orders";
import { formatVND } from "@/lib/utils";

const CHIP_TONES: Record<PosPhase, string> = {
  READY_TO_CLOSE: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300",
  AWAITING_SUBMIT: "bg-amber-100 text-amber-800 dark:bg-amber-950/60 dark:text-amber-300",
  IN_PREPARATION: "bg-primary/10 text-primary",
  AWAITING_PAYMENT: "bg-muted text-foreground",
  DRAFTING: "bg-muted text-muted-foreground",
  NO_SESSION: "bg-muted text-muted-foreground",
};

export interface PendingOrderRowProps {
  order: PendingOrder;
  isActive: boolean;
  nowMs: number;
  onSelect: (sessionId: string) => void;
}

/** Tapping a row reopens that session; the right-hand panel then offers its action. */
export function PendingOrderRow({ order, isActive, nowMs, onSelect }: PendingOrderRowProps) {
  const showProgress = order.phase === "IN_PREPARATION" || order.phase === "READY_TO_CLOSE";
  const percent =
    order.progress.total > 0 ? Math.round((order.progress.done * 100) / order.progress.total) : 0;

  return (
    <button
      type="button"
      onClick={() => onSelect(order.sessionId)}
      aria-current={isActive ? "true" : undefined}
      className={`w-full min-h-[48px] rounded-xl border p-3 text-left space-y-1.5 select-none active:scale-[0.98] transition ${
        isActive ? "border-primary bg-primary/5" : "border-border bg-card hover:bg-muted"
      }`}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-bold text-foreground truncate">
          {order.tableLabel ? `${order.tableLabel} · #${order.serviceNumber}` : `#${order.serviceNumber}`}
        </span>
        <span className={`rounded-md px-2 py-0.5 text-2xs font-bold ${CHIP_TONES[order.phase]}`}>
          {PENDING_PHASE_LABELS[order.phase]}
        </span>
      </div>
      <div className="flex items-baseline justify-between gap-2 text-2xs text-muted-foreground">
        <span className="truncate">{order.itemSummary}</span>
        <span className="font-mono tabular-nums font-semibold text-foreground shrink-0">
          {formatVND(order.totalVnd)}
        </span>
      </div>
      {showProgress && (
        <div className="flex items-center gap-2">
          <div className="h-1.5 flex-1 rounded-full bg-muted overflow-hidden">
            <div className="h-full rounded-full bg-primary" style={{ width: `${percent}%` }} />
          </div>
          <span className="font-mono text-2xs tabular-nums text-muted-foreground">
            {`${order.progress.done}/${order.progress.total} món xong`}
          </span>
        </div>
      )}
      <p className="text-2xs text-muted-foreground">{formatAge(minutesSince(order.createdAt, nowMs))}</p>
    </button>
  );
}
