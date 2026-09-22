import * as React from "react";
import { X, Minus, Plus, Coffee } from "lucide-react";
import type { CatalogSellableItemResponse } from "@/api/generated/models";
import {
  resolveInitialSelection,
  toggleModifierOption,
  isSelectionValid,
  normalizePreparationNote,
  MAX_PREPARATION_NOTE_LENGTH,
} from "../utils/selection";
import { calculateItemUnitPrice, calculateLineTotal } from "../utils/pricing";
import { formatVND } from "@/lib/utils";
import { playTapChirp } from "@/lib/sound";

export interface ItemPickerConfig {
  sizeId?: string;
  selectedOptionIds: string[];
  preparationNote: string;
  quantity: number;
}

export interface ItemPickerDialogProps {
  item: CatalogSellableItemResponse | null;
  initialValues?: Partial<ItemPickerConfig>;
  isOpen: boolean;
  onClose: () => void;
  onConfirm: (config: ItemPickerConfig) => void;
  isSubmitting?: boolean;
  confirmLabel?: string;
}

export function ItemPickerDialog({
  item,
  initialValues,
  isOpen,
  onClose,
  onConfirm,
  isSubmitting = false,
  confirmLabel = "Thêm vào đơn",
}: ItemPickerDialogProps) {
  const [sizeId, setSizeId] = React.useState<string | undefined>(undefined);
  const [selectedOptionIds, setSelectedOptionIds] = React.useState<string[]>([]);
  const [note, setNote] = React.useState<string>("");
  const [quantity, setQuantity] = React.useState<number>(1);

  // Sync state when dialog opens or item changes
  React.useEffect(() => {
    if (isOpen && item) {
      const defaults = resolveInitialSelection(item);
      // oxlint-disable-next-line react/set-state-in-effect
      setSizeId(initialValues?.sizeId ?? defaults.sizeId);
      // oxlint-disable-next-line react/set-state-in-effect
      setSelectedOptionIds(
        initialValues?.selectedOptionIds ?? defaults.selectedOptionIds,
      );
      // oxlint-disable-next-line react/set-state-in-effect
      setNote(initialValues?.preparationNote ?? defaults.note);
      // oxlint-disable-next-line react/set-state-in-effect
      setQuantity(initialValues?.quantity ?? defaults.quantity);
    }
  }, [isOpen, item, initialValues]);

  if (!isOpen || !item) return null;

  const hasSizes = Boolean(item.sizes && item.sizes.length > 0);
  const selectedSize = item.sizes?.find((s) => s.id === sizeId);
  const basePrice = selectedSize?.price_vnd ?? item.price_vnd ?? 0;

  // Calculate option surcharges
  const selectedOptionSurcharges: number[] = [];
  if (item.modifier_groups) {
    for (const group of item.modifier_groups) {
      for (const opt of group.options ?? []) {
        if (opt.id && selectedOptionIds.includes(opt.id)) {
          selectedOptionSurcharges.push(opt.surcharge_vnd ?? 0);
        }
      }
    }
  }

  const unitPrice = calculateItemUnitPrice(basePrice, selectedOptionSurcharges);
  const lineTotal = calculateLineTotal(unitPrice, quantity);
  const isValid = isSelectionValid(item, sizeId, selectedOptionIds);

  const handleConfirm = () => {
    if (!isValid || isSubmitting) return;
    playTapChirp();
    onConfirm({
      sizeId,
      selectedOptionIds,
      preparationNote: normalizePreparationNote(note),
      quantity,
    });
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-xs animate-in fade-in duration-150">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="picker-dialog-title"
        className="flex flex-col w-full max-w-lg max-h-[90vh] rounded-2xl border border-border bg-card shadow-2xl overflow-hidden animate-in fade-in zoom-in-95 duration-150"
      >
        {/* Dialog Header */}
        <div className="flex items-center justify-between border-b border-border p-4 bg-muted/20">
          <div className="flex items-center gap-3">
            <div className="h-10 w-10 rounded-xl bg-primary/10 text-primary flex items-center justify-center">
              <Coffee className="h-5 w-5" />
            </div>
            <div>
              <h3 id="picker-dialog-title" className="text-base font-bold text-foreground truncate">
                {item.name}
              </h3>
              <p className="text-2xs text-muted-foreground">Tùy chọn kích cỡ, topping & ghi chú</p>
            </div>
          </div>
          <button
            type="button"
            onClick={() => {
              playTapChirp();
              onClose();
            }}
            className="h-12 w-12 min-h-[48px] min-w-[48px] rounded-xl text-muted-foreground hover:text-foreground hover:bg-muted flex items-center justify-center select-none active:scale-[0.98] transition"
            aria-label="Đóng"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Dialog Scrollable Content */}
        <div className="flex-1 overflow-y-auto p-5 space-y-5">
          {/* Sizes Selection (>= 48px touch chips) */}
          {hasSizes && (
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
                  Kích cỡ <span className="text-destructive">*</span>
                </span>
                <span className="text-2xs text-muted-foreground">Bắt buộc chọn 1</span>
              </div>
              <div className="grid grid-cols-3 gap-2">
                {item.sizes?.map((size) => {
                  const isSelected = sizeId === size.id;
                  return (
                    <button
                      key={size.id}
                      type="button"
                      onClick={() => {
                        playTapChirp();
                        setSizeId(size.id);
                      }}
                      className={`min-h-[48px] h-12 px-3 rounded-xl border flex flex-col items-center justify-center transition-all select-none active:scale-[0.98] ${
                        isSelected
                          ? "border-primary bg-primary/10 text-primary font-bold ring-2 ring-primary/20"
                          : "border-border bg-card text-foreground hover:bg-muted"
                      }`}
                    >
                      <span className="text-xs font-bold leading-tight">{size.name}</span>
                      <span className="font-mono text-2xs text-muted-foreground">
                        {formatVND(size.price_vnd ?? 0)}
                      </span>
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          {/* Modifier Groups Selection (>= 48px touch chips) */}
          {item.modifier_groups?.map((group) => {
            const min = group.min_selections ?? 0;
            const max = group.max_selections ?? 1;
            const ruleText =
              min > 0 && max === 1
                ? "Bắt buộc chọn 1"
                : min > 0
                  ? `Chọn tối thiểu ${min}, tối đa ${max}`
                  : `Tùy chọn (tối đa ${max})`;

            return (
              <div key={group.id} className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
                    {group.name} {min > 0 && <span className="text-destructive">*</span>}
                  </span>
                  <span className="text-2xs text-muted-foreground">{ruleText}</span>
                </div>
                <div className="grid grid-cols-2 gap-2">
                  {group.options?.map((opt) => {
                    const isSelected = selectedOptionIds.includes(opt.id ?? "");
                    const surcharge = opt.surcharge_vnd ?? 0;

                    return (
                      <button
                        key={opt.id}
                        type="button"
                        onClick={() => {
                          playTapChirp();
                          setSelectedOptionIds((prev) =>
                            toggleModifierOption(group.id ?? "", opt.id ?? "", prev, group),
                          );
                        }}
                        className={`min-h-[48px] h-12 px-3 rounded-xl border flex items-center justify-between text-xs transition-all select-none active:scale-[0.98] ${
                          isSelected
                            ? "border-primary bg-primary/10 text-primary font-bold ring-2 ring-primary/20"
                            : "border-border bg-card text-foreground hover:bg-muted"
                        }`}
                      >
                        <span className="truncate pr-1">{opt.name}</span>
                        {surcharge > 0 && (
                          <span className="font-mono font-semibold text-primary shrink-0">
                            +{formatVND(surcharge)}
                          </span>
                        )}
                      </button>
                    );
                  })}
                </div>
              </div>
            );
          })}

          {/* Preparation Note (max 200 code points) */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <span className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
                Ghi chú pha chế
              </span>
              <span className="text-2xs font-mono text-muted-foreground">
                {Array.from(note).length}/{MAX_PREPARATION_NOTE_LENGTH}
              </span>
            </div>
            <input
              type="text"
              value={note}
              maxLength={MAX_PREPARATION_NOTE_LENGTH}
              onChange={(e) => setNote(e.target.value)}
              placeholder="Ví dụ: Ít ngọt, nhiều đá, để riêng sốt..."
              className="h-12 w-full rounded-xl border border-border bg-card px-3.5 text-sm font-medium text-foreground placeholder:text-muted-foreground focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20"
            />
          </div>
        </div>

        {/* Dialog Footer (Quantity Stepper & Confirm CTA) */}
        <div className="flex items-center justify-between border-t border-border p-4 bg-muted/20 gap-4">
          {/* Stepper (min-w-[48px] min-h-[48px]) */}
          <div className="flex items-center rounded-xl border border-border bg-card p-0.5">
            <button
              type="button"
              disabled={quantity <= 1}
              onClick={() => {
                playTapChirp();
                setQuantity((q) => Math.max(1, q - 1));
              }}
              className="h-12 w-12 min-h-[48px] min-w-[48px] rounded-lg text-foreground hover:bg-muted disabled:opacity-30 disabled:hover:bg-transparent flex items-center justify-center select-none active:scale-[0.98] transition"
              aria-label="Giảm số lượng"
            >
              <Minus className="h-4 w-4" />
            </button>
            <span className="w-10 text-center font-mono text-base font-bold text-foreground tabular-nums">
              {quantity}
            </span>
            <button
              type="button"
              disabled={quantity >= 9999}
              onClick={() => {
                playTapChirp();
                setQuantity((q) => Math.min(9999, q + 1));
              }}
              className="h-12 w-12 min-h-[48px] min-w-[48px] rounded-lg text-foreground hover:bg-muted disabled:opacity-30 disabled:hover:bg-transparent flex items-center justify-center select-none active:scale-[0.98] transition"
              aria-label="Tăng số lượng"
            >
              <Plus className="h-4 w-4" />
            </button>
          </div>

          {/* Confirm Button */}
          <button
            type="button"
            disabled={!isValid || isSubmitting}
            onClick={handleConfirm}
            className="min-h-[48px] h-12 flex-1 rounded-xl bg-primary hover:bg-primary/90 disabled:bg-muted disabled:text-muted-foreground text-primary-foreground font-bold text-sm shadow-xs transition-all active:scale-[0.98] flex items-center justify-between px-5 select-none"
          >
            <span>{confirmLabel}</span>
            <span className="font-mono text-base font-bold tabular-nums">
              {formatVND(lineTotal)}
            </span>
          </button>
        </div>
      </div>
    </div>
  );
}
