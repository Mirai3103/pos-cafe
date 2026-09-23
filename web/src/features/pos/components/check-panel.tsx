import { Receipt, AlertTriangle, CheckCircle2, ChefHat } from "lucide-react";
import type { SalesServiceSessionResponse } from "@/api/generated/models";
import {
  listLiveChecks,
  selectOpenCheck,
  hasMultipleOpenChecks,
  type PosPhase,
} from "../utils/phase";
import { latestPaymentChangeDue } from "../utils/payment";
import { formatVND } from "@/lib/utils";

export interface CheckPanelProps {
  session: SalesServiceSessionResponse | null;
  phase: PosPhase;
  onCollect: () => void;
  onNextCustomer: () => void;
  className?: string;
}

/**
 * The Order Bill once the draft is committed: a read-only Check, then the
 * settled receipt.
 *
 * Every amount here is the server's frozen snapshot. Nothing on this panel is
 * recomputed from the catalog, because the customer is charged what the Check
 * says and not what the menu says today.
 */
export function CheckPanel({
  session,
  phase,
  onCollect,
  onNextCustomer,
  className,
}: CheckPanelProps) {
  const asideLayout =
    className ?? "w-full md:w-[380px] lg:w-[420px] shrink-0 border-l border-border";

  const isSettled = phase === "SETTLED";
  const openCheck = selectOpenCheck(session);
  const checks = listLiveChecks(session);
  const check = openCheck ?? checks[0] ?? null;
  const ambiguous = hasMultipleOpenChecks(session);

  const allocations = check?.allocations ?? [];
  const totalApplied = checks.reduce((sum, c) => sum + (c.total_applied_vnd ?? 0), 0);
  const changeGiven = latestPaymentChangeDue(check);
  const serviceNumber = session?.service_number;

  return (
    <aside className={`flex flex-col bg-card overflow-hidden select-none ${asideLayout}`}>
      {/* Header */}
      <div className="flex items-center justify-between border-b border-border p-4 bg-muted/20 shrink-0">
        <div className="flex items-center gap-2.5">
          <div className="h-9 w-9 rounded-xl bg-primary/10 text-primary flex items-center justify-center">
            <Receipt className="h-5 w-5" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="text-sm font-bold text-foreground">
                {serviceNumber ? `Đơn mang đi #${serviceNumber}` : "Đơn mang đi"}
              </span>
              <span className="rounded-md bg-emerald-100 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300 border border-emerald-200/60 px-2 py-0.5 text-2xs font-bold">
                {isSettled ? "Đã thanh toán" : "Đã chốt"}
              </span>
            </div>
            <p className="text-2xs text-muted-foreground mt-0.5">
              {isSettled ? "Đơn đã thu đủ tiền" : "Đơn đã chốt giá, chờ thu tiền"}
            </p>
          </div>
        </div>
      </div>

      {/* Committed items, read-only */}
      <div className="flex-1 overflow-y-auto p-4 space-y-2.5 bg-muted/10">
        {allocations.map((allocation) => (
          <div
            key={allocation.id}
            className="rounded-xl border border-border bg-card p-3 space-y-1"
          >
            <div className="flex items-start justify-between gap-2">
              <span className="text-sm font-bold text-foreground">{allocation.name}</span>
              <span className="font-mono text-sm font-bold text-foreground tabular-nums shrink-0">
                {formatVND(allocation.amount_vnd ?? 0)}
              </span>
            </div>
            <p className="text-2xs text-muted-foreground">
              {[
                `SL ${allocation.allocated_quantity ?? 0}`,
                allocation.size_name,
                ...(allocation.modifiers ?? []).map((m) => m.option_name),
              ]
                .filter(Boolean)
                .join(" · ")}
            </p>
            {allocation.preparation_note && (
              <p className="text-2xs italic text-muted-foreground">
                Ghi chú: {allocation.preparation_note}
              </p>
            )}
          </div>
        ))}
      </div>

      {ambiguous && (
        <div
          role="alert"
          className="mx-4 mb-2 flex items-start gap-2 rounded-xl bg-destructive/10 text-destructive px-3 py-2.5 text-xs font-semibold"
        >
          <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5" />
          <span>
            Phiên này có nhiều hóa đơn đang mở. Chức năng tách hóa đơn chưa hỗ trợ ở
            phiên bản này, vui lòng báo quản lý.
          </span>
        </div>
      )}

      {/* Financial summary */}
      <div className="border-t border-border bg-card p-4 space-y-1.5 shrink-0 shadow-2xs">
        {isSettled ? (
          <>
            <div className="flex items-baseline justify-between text-xs text-muted-foreground">
              <span>Đã thu</span>
              <span className="font-mono tabular-nums font-semibold text-foreground">
                {formatVND(totalApplied)}
              </span>
            </div>
            <div className="flex items-baseline justify-between pt-1 border-t border-border/50">
              <span className="text-sm font-bold text-foreground">Tiền thối</span>
              <span className="font-mono text-2xl font-bold text-primary tabular-nums">
                {formatVND(changeGiven)}
              </span>
            </div>
          </>
        ) : (
          <div className="flex items-baseline justify-between pt-1">
            <span className="text-sm font-bold text-foreground">Còn phải thu</span>
            <span className="font-mono text-2xl font-bold text-primary tabular-nums">
              {formatVND(check?.balance_vnd ?? 0)}
            </span>
          </div>
        )}
      </div>

      {/* Actions */}
      <div className="border-t border-border bg-muted/20 p-4 space-y-2.5 shrink-0">
        {isSettled ? (
          <>
            <button
              type="button"
              onClick={onNextCustomer}
              className="min-h-[48px] h-12 w-full rounded-xl bg-primary text-sm font-bold text-primary-foreground flex items-center justify-center gap-2 select-none active:scale-[0.98] transition"
            >
              <CheckCircle2 className="h-4 w-4" />
              <span>Khách tiếp theo (F9)</span>
            </button>
            <button
              type="button"
              disabled
              title="Gửi bếp sẽ hoạt động ở Slice 5"
              className="min-h-[48px] h-12 w-full rounded-xl bg-muted text-muted-foreground font-bold text-sm cursor-not-allowed border border-border flex flex-col items-center justify-center opacity-60"
            >
              <span className="flex items-center gap-2">
                <ChefHat className="h-4 w-4" />
                Gửi bếp
              </span>
              <span className="text-2xs font-normal text-muted-foreground">
                Mở ở Slice 5
              </span>
            </button>
          </>
        ) : (
          <button
            type="button"
            onClick={onCollect}
            disabled={ambiguous || !openCheck}
            className="min-h-[48px] h-12 w-full rounded-xl bg-primary text-sm font-bold text-primary-foreground disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center select-none active:scale-[0.98] transition"
          >
            Thu tiền (F9)
          </button>
        )}
      </div>
    </aside>
  );
}
