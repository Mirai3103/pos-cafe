import { Minus, Plus, Trash2, AlertCircle } from "lucide-react";
import type { SalesDraftItemResponse } from "@/api/generated/models";
import { calculateItemUnitPrice, calculateLineTotal } from "../utils/pricing";
import { formatVND } from "@/lib/utils";
import { playTapChirp } from "@/lib/sound";

interface DraftItemRowProps {
  item: SalesDraftItemResponse;
  onEdit: (item: SalesDraftItemResponse) => void;
  onQuantityChange: (itemId: string, nextQty: number) => void;
  onRemove: (itemId: string) => void;
  disabled?: boolean;
}

export function DraftItemRow({
  item,
  onEdit,
  onQuantityChange,
  onRemove,
  disabled = false,
}: DraftItemRowProps) {
  const basePrice = item.price_vnd ?? 0;
  const surcharges = (item.selected_modifier_options ?? []).map(
    (opt) => opt.surcharge_vnd ?? 0,
  );
  const unitPrice = calculateItemUnitPrice(basePrice, surcharges);
  const quantity = item.quantity ?? 1;
  const lineTotal = calculateLineTotal(unitPrice, quantity);
  const isAvailable = item.available ?? true;

  const modifierSummary = (item.selected_modifier_options ?? [])
    .map((opt) => opt.name)
    .filter(Boolean)
    .join(", ");

  return (
    <div className="flex flex-col rounded-xl border border-border bg-card p-3 shadow-2xs gap-2 transition-all">
      {/* Top Header Row: Name, Size, Delete */}
      <div className="flex items-start justify-between gap-2">
        <div
          role="button"
          tabIndex={0}
          onClick={() => {
            if (!disabled) {
              playTapChirp();
              onEdit(item);
            }
          }}
          onKeyDown={(e) => {
            if (!disabled && (e.key === "Enter" || e.key === " ")) {
              e.preventDefault();
              playTapChirp();
              onEdit(item);
            }
          }}
          className="flex-1 cursor-pointer select-none space-y-0.5"
        >
          <div className="flex items-center gap-1.5 flex-wrap">
            <h5 className="text-sm font-bold text-foreground hover:text-primary transition-colors">
              {item.name}
            </h5>
            {item.size_name && (
              <span className="rounded bg-muted px-1.5 py-0.5 text-2xs font-semibold text-muted-foreground border border-border">
                {item.size_name}
              </span>
            )}
            {!isAvailable && (
              <span className="inline-flex items-center gap-1 rounded bg-amber-500/10 text-amber-700 dark:text-amber-400 border border-amber-200 px-1.5 py-0.5 text-2xs font-bold">
                <AlertCircle className="h-3 w-3" />
                Tạm hết hàng
              </span>
            )}
          </div>

          {/* Modifier List */}
          {modifierSummary && (
            <p className="text-2xs text-muted-foreground leading-tight">
              {modifierSummary}
            </p>
          )}

          {/* Preparation Note */}
          {item.preparation_note && (
            <p className="text-2xs italic text-slate-500 dark:text-slate-400">
              "{item.preparation_note}"
            </p>
          )}
        </div>

        {/* Delete Row Button */}
        <button
          type="button"
          disabled={disabled}
          onClick={() => {
            playTapChirp();
            if (item.id) onRemove(item.id);
          }}
          className="h-10 w-10 min-h-[40px] min-w-[40px] rounded-lg text-muted-foreground hover:text-destructive hover:bg-destructive/10 flex items-center justify-center select-none active:scale-95 transition"
          aria-label="Xóa món"
        >
          <Trash2 className="h-4 w-4" />
        </button>
      </div>

      {/* Bottom Line: Unit Price, Stepper, Line Total */}
      <div className="flex items-center justify-between pt-1 border-t border-border/40">
        <span className="font-mono text-xs text-muted-foreground">
          {formatVND(unitPrice)}
        </span>

        {/* Stepper */}
        <div className="flex items-center gap-3">
          <div className="flex items-center rounded-lg border border-border bg-muted/40 p-0.5">
            <button
              type="button"
              disabled={disabled || quantity <= 1}
              onClick={() => {
                playTapChirp();
                if (item.id) onQuantityChange(item.id, Math.max(1, quantity - 1));
              }}
              className="h-8 w-8 min-h-[32px] min-w-[32px] rounded text-foreground hover:bg-card disabled:opacity-30 flex items-center justify-center select-none active:scale-90 transition"
              aria-label="Giảm số lượng"
            >
              <Minus className="h-3.5 w-3.5" />
            </button>
            <span className="w-8 text-center font-mono text-xs font-bold text-foreground tabular-nums">
              {quantity}
            </span>
            <button
              type="button"
              disabled={disabled || quantity >= 9999}
              onClick={() => {
                playTapChirp();
                if (item.id) onQuantityChange(item.id, Math.min(9999, quantity + 1));
              }}
              className="h-8 w-8 min-h-[32px] min-w-[32px] rounded text-foreground hover:bg-card disabled:opacity-30 flex items-center justify-center select-none active:scale-90 transition"
              aria-label="Tăng số lượng"
            >
              <Plus className="h-3.5 w-3.5" />
            </button>
          </div>

          <span className="font-mono text-sm font-bold text-foreground tabular-nums">
            {formatVND(lineTotal)}
          </span>
        </div>
      </div>
    </div>
  );
}
