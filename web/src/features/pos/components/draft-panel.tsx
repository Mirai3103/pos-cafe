import { Receipt, Trash2, ShoppingBag, Utensils, Banknote, QrCode } from "lucide-react";
import type {
  SalesServiceSessionResponse,
  SalesDraftItemResponse,
} from "@/api/generated/models";
import { DraftEmptyState } from "./draft-empty-state";
import { DraftItemRow } from "./draft-item-row";
import { NoShiftNotice } from "./no-shift-notice";
import { calculateDraftSubtotal } from "../utils/pricing";
import { formatVND } from "@/lib/utils";

interface DraftPanelProps {
  session: SalesServiceSessionResponse | null;
  isShiftOpen: boolean;
  onEditItem: (item: SalesDraftItemResponse) => void;
  onQuantityChange: (itemId: string, nextQty: number) => void;
  onRemoveItem: (itemId: string) => void;
  disabled?: boolean;
  className?: string;
}

export function DraftPanel({
  session,
  isShiftOpen,
  onEditItem,
  onQuantityChange,
  onRemoveItem,
  disabled = false,
  className,
}: DraftPanelProps) {
  const asideLayout = className ?? "w-full md:w-[380px] lg:w-[420px] shrink-0 border-l border-border";

  if (!isShiftOpen) {
    return (
      <aside className={`flex flex-col bg-card overflow-hidden select-none ${asideLayout}`}>
        <NoShiftNotice />
      </aside>
    );
  }

  const draftItems = session?.draft?.items ?? [];
  const subtotal = calculateDraftSubtotal(draftItems);
  const serviceNumber = session?.service_number;

  return (
    <aside className={`flex flex-col bg-card overflow-hidden select-none ${asideLayout}`}>
      {/* Bill Header */}
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
                Mang đi
              </span>
            </div>
            <p className="text-2xs text-muted-foreground mt-0.5">Khách mua mang về</p>
          </div>
        </div>

        {/* Clear / Reset Cart (Disabled in Slice 3) */}
        <button
          type="button"
          disabled
          title="Chức năng hủy toàn bộ đơn chưa hỗ trợ; vui lòng xóa từng món"
          className="h-12 min-h-[48px] px-3 rounded-xl text-xs font-semibold text-muted-foreground border border-transparent opacity-40 cursor-not-allowed flex items-center gap-1.5"
        >
          <Trash2 className="h-4 w-4" />
          <span>Hủy đơn</span>
        </button>
      </div>

      {/* Bill Items Scrollable Container */}
      <div className="flex-1 overflow-y-auto p-4 space-y-2.5 bg-muted/10">
        {draftItems.length > 0 ? (
          draftItems.map((item) => (
            <DraftItemRow
              key={item.id}
              item={item}
              onEdit={onEditItem}
              onQuantityChange={onQuantityChange}
              onRemove={onRemoveItem}
              disabled={disabled}
            />
          ))
        ) : (
          <DraftEmptyState />
        )}
      </div>

      {/* Financial Summary */}
      <div className="border-t border-border bg-card p-4 space-y-1.5 shrink-0 shadow-2xs">
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>Tạm tính ({draftItems.length} món)</span>
          <span className="font-mono font-semibold text-foreground">{formatVND(subtotal)}</span>
        </div>
        <div className="flex items-baseline justify-between pt-1 border-t border-border/50">
          <span className="text-sm font-bold text-foreground">Tổng cộng</span>
          <span className="font-mono text-2xl font-bold text-primary tabular-nums">
            {formatVND(subtotal)}
          </span>
        </div>
      </div>

      {/* Bill Actions / Checkout Drawer (Disabled in Slice 3) */}
      <div className="border-t border-border bg-muted/20 p-4 space-y-2.5 shrink-0">
        {/* Mode Switcher: Dine-In Disabled */}
        <div className="grid grid-cols-2 gap-2 p-1 bg-muted rounded-xl border border-border">
          <div className="min-h-[40px] h-10 px-3 rounded-lg font-bold text-xs bg-foreground text-background flex items-center justify-center gap-1.5 shadow-xs">
            <ShoppingBag className="h-3.5 w-3.5" />
            <span>Mang đi</span>
          </div>
          <button
            type="button"
            disabled
            className="min-h-[40px] h-10 px-3 rounded-lg font-medium text-xs text-muted-foreground opacity-50 cursor-not-allowed flex items-center justify-center gap-1.5"
            title="Chế độ Tại bàn sẽ hoạt động ở Slice 7"
          >
            <Utensils className="h-3.5 w-3.5" />
            <span>Tại bàn (F2)</span>
          </button>
        </div>

        {/* Payment Tabs (Disabled) */}
        <div className="grid grid-cols-2 gap-2 p-1 bg-muted rounded-xl border border-border opacity-50">
          <div className="min-h-[40px] h-10 px-3 rounded-lg font-bold text-xs bg-card text-foreground flex items-center justify-center gap-1.5 shadow-2xs">
            <Banknote className="h-3.5 w-3.5" />
            <span>Tiền mặt</span>
          </div>
          <div className="min-h-[40px] h-10 px-3 rounded-lg font-medium text-xs text-muted-foreground flex items-center justify-center gap-1.5">
            <QrCode className="h-3.5 w-3.5" />
            <span>VietQR</span>
          </div>
        </div>

        {/* Checkout Commitment CTA (Disabled in Slice 3) */}
        <button
          type="button"
          disabled
          className="min-h-[48px] h-12 w-full rounded-xl bg-muted text-muted-foreground font-bold text-sm cursor-not-allowed border border-border flex flex-col items-center justify-center opacity-60"
        >
          <span>Thanh toán (F9)</span>
          <span className="text-2xs font-normal text-muted-foreground">Mở ở Slice 4</span>
        </button>
      </div>
    </aside>
  );
}
