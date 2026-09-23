import { CheckCircle2, ChefHat } from "lucide-react";
import { useHotkeys } from "react-hotkeys-hook";
import type { SalesCompletedSaleResponse } from "@/api/generated/models";
import { formatVND } from "@/lib/utils";
import { playSuccessChirp } from "@/lib/sound";
import {
  completedSaleItems,
  completedSaleTotals,
  completedSalePayments,
  summarizePreparation,
  formatCompletedAt,
} from "../utils/completed-sale";

export interface CompletedSaleDialogProps {
  sale: SalesCompletedSaleResponse | null;
  onDone: () => void;
}

/** The immutable record of a closed sale, shown once before the next customer. */
export function CompletedSaleDialog({ sale, onDone }: CompletedSaleDialogProps) {
  const handleDone = () => {
    playSuccessChirp();
    onDone();
  };

  useHotkeys(
    "enter, f9, escape",
    (event) => {
      event.preventDefault();
      handleDone();
    },
    { enabled: sale !== null, enableOnFormTags: true },
  );

  if (!sale) return null;

  const items = completedSaleItems(sale);
  const totals = completedSaleTotals(sale);
  const payments = completedSalePayments(sale);
  const byline = [formatCompletedAt(sale.completed_at), sale.completed_by_display_name]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-xs animate-in fade-in duration-150">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="completed-sale-title"
        className="flex flex-col w-full max-w-md max-h-[90vh] rounded-2xl border border-border bg-card shadow-2xl overflow-hidden animate-in fade-in zoom-in-95 duration-150"
      >
        <div className="flex items-center gap-3 border-b border-border p-4 bg-muted/20 shrink-0">
          <div className="h-10 w-10 rounded-xl bg-emerald-100 text-emerald-700 dark:bg-emerald-950/60 dark:text-emerald-300 flex items-center justify-center">
            <CheckCircle2 className="h-5 w-5" />
          </div>
          <div>
            <h3 id="completed-sale-title" className="text-base font-bold text-foreground">
              {`Đơn #${sale.service_number ?? ""} đã hoàn tất`}
            </h3>
            {byline && <p className="text-2xs text-muted-foreground">{byline}</p>}
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-4 space-y-2">
          {items.map((item) => (
            <div key={item.id} className="flex items-baseline justify-between gap-2 text-sm">
              <span className="text-foreground">
                <span className="font-mono tabular-nums text-muted-foreground mr-2">
                  {item.allocated_quantity ?? 0}x
                </span>
                {item.name}
              </span>
              <span className="font-mono tabular-nums font-semibold text-foreground shrink-0">
                {formatVND(item.amount_vnd ?? 0)}
              </span>
            </div>
          ))}
        </div>

        <div className="border-t border-border p-4 space-y-1.5 shrink-0">
          <div className="flex items-baseline justify-between">
            <span className="text-sm font-bold text-foreground">Tổng cộng</span>
            <span className="font-mono text-xl font-bold text-primary tabular-nums">
              {formatVND(totals.chargeVnd)}
            </span>
          </div>
          <div className="flex items-baseline justify-between text-xs text-muted-foreground">
            <span>Đã nhận</span>
            <span className="font-mono tabular-nums font-semibold text-foreground">
              {formatVND(totals.receivedVnd)}
            </span>
          </div>
          {payments.map((payment) => (
            <div
              key={payment.id}
              className="flex items-baseline justify-between text-xs text-muted-foreground"
            >
              <span>{payment.method === "CASH" ? "Tiền mặt" : "Chuyển khoản"}</span>
              <span className="font-mono tabular-nums">
                {`Khách đưa ${formatVND(payment.cash_tendered_vnd ?? payment.applied_amount_vnd ?? 0)} · Thối ${formatVND(payment.change_due_vnd ?? 0)}`}
              </span>
            </div>
          ))}
          <p className="flex items-center gap-1.5 pt-1 text-xs text-muted-foreground">
            <ChefHat className="h-3.5 w-3.5" />
            {summarizePreparation(sale.preparation_units)}
          </p>
        </div>

        <div className="border-t border-border bg-muted/20 p-4 shrink-0">
          <button
            type="button"
            onClick={handleDone}
            className="min-h-[48px] w-full rounded-xl bg-primary text-sm font-bold text-primary-foreground select-none active:scale-[0.98] transition"
          >
            Xong (Enter)
          </button>
        </div>
      </div>
    </div>
  );
}
