import { AlertCircle, AlertTriangle } from "lucide-react";
import type { SalesDraftItemResponse, SalesServiceSessionResponse } from "@/api/generated/models";
import { formatVND } from "@/lib/utils";
import { DraftItemRow } from "./draft-item-row";
import { DineInHeader } from "./dine-in-header";
import { DineInActions } from "./dine-in-actions";
import { listLiveChecks } from "../utils/phase";
import { calculateDraftSubtotal } from "../utils/pricing";
import type { DineInStatus } from "../utils/dine-in";

export interface DineInPanelProps {
  session: SalesServiceSessionResponse;
  status: DineInStatus;
  isShiftOpen: boolean;
  sendError: string | null;
  isSending: boolean;
  isClosing: boolean;
  onEditItem: (item: SalesDraftItemResponse) => void;
  onQuantityChange: (itemId: string, nextQty: number) => void;
  onRemoveItem: (itemId: string) => void;
  onSend: () => void;
  onCollect: () => void;
  onClose: () => void;
  onLeave: () => void;
  onChangeTables: () => void;
  className?: string;
}

/**
 * The seated party's bill: the round being drafted, then what the bar already
 * has, then what is owed. Committed amounts are the server's frozen snapshot;
 * only the drafted round is priced locally, for display.
 */
export function DineInPanel(props: DineInPanelProps) {
  const { session, status, className } = props;
  const draftItems = status.hasEditableDraft ? (session.draft?.items ?? []) : [];
  const checks = listLiveChecks(session);
  // Only what the kitchen actually has; a committed-but-unsubmitted round
  // (submit failed, or hasn't run yet) is not "Đã gửi bếp" yet.
  const allocations = checks.flatMap((c) => c.allocations ?? []).filter((a) => a.submitted);
  const owed = checks.reduce((sum, c) => sum + (c.balance_vnd ?? 0), 0);
  const { done, total } = status.progress;

  return (
    <aside className={`flex flex-col bg-card overflow-hidden select-none ${className ?? ""}`}>
      <DineInHeader session={session} onChangeTables={props.onChangeTables} />

      <div className="flex-1 overflow-y-auto p-4 space-y-4 bg-muted/10">
        <section className="space-y-2">
          <h3 className="text-2xs font-bold uppercase tracking-wider text-muted-foreground">Lượt đang gọi</h3>
          {!status.canOrder && (
            <p className="rounded-xl bg-amber-100 text-amber-900 dark:bg-amber-950/60 dark:text-amber-200 px-3 py-2 text-xs font-semibold">
              Gửi bếp lượt trước để gọi thêm
            </p>
          )}
          {draftItems.length > 0 ? (
            draftItems.map((item) => (
              <DraftItemRow key={item.id} item={item} onEdit={props.onEditItem} onQuantityChange={props.onQuantityChange} onRemove={props.onRemoveItem} disabled={!props.isShiftOpen} />
            ))
          ) : (
            status.canOrder && <p className="text-xs text-muted-foreground">Chọn món để bắt đầu lượt mới</p>
          )}
          {draftItems.length > 0 && (
            <p className="text-right font-mono text-xs text-muted-foreground">{`Tạm tính ${formatVND(calculateDraftSubtotal(draftItems))}`}</p>
          )}
        </section>

        {allocations.length > 0 && (
          <section className="space-y-2">
            <div className="flex items-center justify-between">
              <h3 className="text-2xs font-bold uppercase tracking-wider text-muted-foreground">Đã gửi bếp</h3>
              {total > 0 && <span className="font-mono text-2xs tabular-nums text-muted-foreground">{`${done}/${total} món xong`}</span>}
            </div>
            {allocations.map((a) => (
              <div key={a.id} className="flex items-start justify-between gap-2 rounded-xl border border-border bg-card p-3">
                <span className="text-sm font-semibold">{`${a.allocated_quantity ?? 0} × ${a.name ?? ""}`}</span>
                <span className="font-mono text-sm tabular-nums">{formatVND(a.amount_vnd ?? 0)}</span>
              </div>
            ))}
          </section>
        )}
      </div>

      {status.hasMultipleOpenChecks && (
        <div role="alert" className="mx-4 mb-2 flex items-start gap-2 rounded-xl bg-destructive/10 text-destructive px-3 py-2.5 text-xs font-semibold">
          <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5" />
          <span>Phiên này có nhiều hóa đơn đang mở. Chức năng tách hóa đơn chưa hỗ trợ ở phiên bản này, vui lòng báo quản lý.</span>
        </div>
      )}
      {props.sendError && (
        <div role="alert" className="mx-4 mb-2 flex items-start gap-2 rounded-xl bg-amber-100 text-amber-900 dark:bg-amber-950/60 dark:text-amber-200 px-3 py-2.5 text-xs font-semibold">
          <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
          <span>{`Chưa gửi được bếp. ${props.sendError}`}</span>
        </div>
      )}

      <div className="border-t border-border bg-card p-4 flex items-baseline justify-between shrink-0">
        <span className="text-sm font-bold">Còn phải thu</span>
        <span className="font-mono text-2xl font-bold text-primary tabular-nums">{formatVND(owed)}</span>
      </div>

      <DineInActions
        status={status}
        isShiftOpen={props.isShiftOpen}
        isSending={props.isSending}
        isClosing={props.isClosing}
        onSend={props.onSend}
        onCollect={props.onCollect}
        onClose={props.onClose}
        onLeave={props.onLeave}
      />
    </aside>
  );
}
