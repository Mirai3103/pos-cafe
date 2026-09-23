import { Receipt, AlertTriangle, AlertCircle, ChefHat } from "lucide-react";
import type { SalesServiceSessionResponse } from "@/api/generated/models";
import {
  listLiveChecks,
  selectOpenCheck,
  hasMultipleOpenChecks,
  isPostPaymentPhase,
  preparationProgress,
  type PosPhase,
} from "../utils/phase";
import { latestPaymentChangeDue } from "../utils/payment";
import { formatVND } from "@/lib/utils";
import { CheckPanelActions } from "./check-panel-actions";

export interface CheckPanelProps {
  session: SalesServiceSessionResponse | null;
  phase: PosPhase;
  onCollect: () => void;
  onSubmit: () => void;
  onClose: () => void;
  onNextCustomer: () => void;
  isSubmitting: boolean;
  isClosing: boolean;
  submitError: string | null;
  className?: string;
}

const HEADINGS: Partial<Record<PosPhase, { badge: string; caption: string }>> = {
  AWAITING_PAYMENT: { badge: "Đã chốt", caption: "Đơn đã chốt giá, chờ thu tiền" },
  AWAITING_SUBMIT: { badge: "Chờ gửi bếp", caption: "Đã thu tiền, chưa gửi bếp" },
  IN_PREPARATION: { badge: "Đang pha chế", caption: "Bếp đang làm món" },
  READY_TO_CLOSE: { badge: "Sẵn sàng hoàn tất", caption: "Bếp đã xong, có thể hoàn tất đơn" },
};

/**
 * The Order Bill once the draft is committed: a read-only Check, then the
 * kitchen's progress, then closure.
 *
 * Every amount here is the server's frozen snapshot. Nothing on this panel is
 * recomputed from the catalog, because the customer is charged what the Check
 * says and not what the menu says today.
 */
export function CheckPanel({
  session,
  phase,
  onCollect,
  onSubmit,
  onClose,
  onNextCustomer,
  isSubmitting,
  isClosing,
  submitError,
  className,
}: CheckPanelProps) {
  const asideLayout =
    className ?? "w-full md:w-[380px] lg:w-[420px] shrink-0 border-l border-border";

  const isSettled = isPostPaymentPhase(phase);
  const openCheck = selectOpenCheck(session);
  const checks = listLiveChecks(session);
  const check = openCheck ?? checks[0] ?? null;
  const ambiguous = hasMultipleOpenChecks(session);

  const allocations = check?.allocations ?? [];
  const totalApplied = checks.reduce((sum, c) => sum + (c.total_applied_vnd ?? 0), 0);
  const changeGiven = latestPaymentChangeDue(check);
  const serviceNumber = session?.service_number;
  const heading = HEADINGS[phase];

  const showProgress = phase === "IN_PREPARATION" || phase === "READY_TO_CLOSE";
  const progress = preparationProgress(session);
  const percent = progress.total > 0 ? Math.round((progress.done * 100) / progress.total) : 0;

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
              {heading && (
                <span className="rounded-md bg-emerald-100 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300 border border-emerald-200/60 px-2 py-0.5 text-2xs font-bold">
                  {heading.badge}
                </span>
              )}
            </div>
            {heading && <p className="text-2xs text-muted-foreground mt-0.5">{heading.caption}</p>}
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

      {phase === "AWAITING_SUBMIT" && submitError && (
        <div
          role="alert"
          className="mx-4 mb-2 flex items-start gap-2 rounded-xl bg-amber-100 text-amber-900 dark:bg-amber-950/60 dark:text-amber-200 px-3 py-2.5 text-xs font-semibold"
        >
          <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
          <span>Chưa gửi được bếp. {submitError}</span>
        </div>
      )}

      {/* Kitchen progress */}
      {showProgress && (
        <div className="border-t border-border bg-card px-4 py-3 space-y-1.5 shrink-0">
          <div className="flex items-center justify-between text-xs">
            <span className="flex items-center gap-1.5 font-semibold text-foreground">
              <ChefHat className="h-3.5 w-3.5" />
              Pha chế
            </span>
            <span className="font-mono tabular-nums text-muted-foreground">
              {`Đã xong ${progress.done}/${progress.total} món`}
            </span>
          </div>
          <div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
            <div className="h-full rounded-full bg-primary transition-all" style={{ width: `${percent}%` }} />
          </div>
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

      <CheckPanelActions
        phase={phase}
        canCollect={!ambiguous && Boolean(openCheck)}
        isSubmitting={isSubmitting}
        isClosing={isClosing}
        onCollect={onCollect}
        onSubmit={onSubmit}
        onClose={onClose}
        onNextCustomer={onNextCustomer}
      />
    </aside>
  );
}
