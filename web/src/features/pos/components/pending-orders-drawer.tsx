import { ClipboardList, RefreshCw, AlertCircle } from "lucide-react";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import type { PendingOrder } from "../utils/pending-orders";
import { PendingOrderRow } from "./pending-order-row";

export interface PendingOrdersButtonProps {
  count: number;
  readyCount: number;
  onClick: () => void;
}

export function PendingOrdersButton({ count, readyCount, onClick }: PendingOrdersButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="min-h-[48px] rounded-xl border border-border bg-card px-4 text-sm font-bold text-foreground hover:bg-muted flex items-center gap-2 select-none active:scale-[0.98] transition"
    >
      <ClipboardList className="h-4 w-4" />
      <span>{`Đơn đang chờ (${count})`}</span>
      {readyCount > 0 && (
        <span
          aria-label={`${readyCount} đơn sẵn sàng hoàn tất`}
          className="min-w-5 h-5 rounded-full bg-emerald-600 px-1.5 text-2xs font-bold text-white flex items-center justify-center tabular-nums"
        >
          {readyCount}
        </span>
      )}
      <span className="text-2xs font-normal text-muted-foreground">F4</span>
    </button>
  );
}

export interface PendingOrdersListProps {
  orders: PendingOrder[];
  isLoading: boolean;
  errorMessage: string | null;
  activeSessionId: string | null;
  /** When the list was last fetched; ages are measured from it so render stays pure. */
  nowMs: number;
  onSelect: (sessionId: string) => void;
  onRetry: () => void;
}

/** The drawer body: loading, error, empty, or one row per order. */
export function PendingOrdersList({
  orders,
  isLoading,
  errorMessage,
  activeSessionId,
  nowMs,
  onSelect,
  onRetry,
}: PendingOrdersListProps) {
  if (isLoading) {
    return (
      <div className="space-y-2">
        {[0, 1, 2].map((key) => (
          <div key={key} className="h-20 rounded-xl bg-muted animate-pulse" />
        ))}
      </div>
    );
  }

  if (errorMessage) {
    return (
      <div className="space-y-3">
        <div
          role="alert"
          className="flex items-start gap-2 rounded-xl bg-destructive/10 text-destructive px-3 py-2.5 text-xs font-semibold"
        >
          <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
          <span>{errorMessage}</span>
        </div>
        <button
          type="button"
          onClick={onRetry}
          className="min-h-[48px] w-full rounded-xl border border-border bg-card text-sm font-bold text-foreground hover:bg-muted flex items-center justify-center gap-2 select-none active:scale-[0.98] transition"
        >
          <RefreshCw className="h-4 w-4" />
          Thử lại
        </button>
      </div>
    );
  }

  if (orders.length === 0) {
    return (
      <p className="py-12 text-center text-sm text-muted-foreground">Không có đơn nào đang chờ</p>
    );
  }

  return (
    <div className="space-y-2">
      {orders.map((order) => (
        <PendingOrderRow
          key={order.sessionId}
          order={order}
          isActive={order.sessionId === activeSessionId}
          nowMs={nowMs}
          onSelect={onSelect}
        />
      ))}
    </div>
  );
}

export interface PendingOrdersDrawerProps extends PendingOrdersListProps {
  isOpen: boolean;
  onClose: () => void;
}

/**
 * Every active takeaway session. It has no action buttons of its own:
 * reopening a session hands it to the Check panel, so each action has one path.
 */
export function PendingOrdersDrawer({ isOpen, onClose, ...listProps }: PendingOrdersDrawerProps) {
  return (
    <Sheet open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="w-full sm:max-w-sm gap-0 p-0">
        <SheetHeader className="border-b border-border p-4 bg-muted/20">
          <SheetTitle className="text-base font-bold">
            {`Đơn đang chờ (${listProps.orders.length})`}
          </SheetTitle>
        </SheetHeader>
        <div className="flex-1 overflow-y-auto p-3">
          <PendingOrdersList {...listProps} />
        </div>
      </SheetContent>
    </Sheet>
  );
}
