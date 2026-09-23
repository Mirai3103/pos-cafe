import * as React from "react";
import { Clock, AlertCircle, RefreshCw } from "lucide-react";
import { useHotkeys } from "react-hotkeys-hook";
import { Button } from "@/components/ui/button";
import { useCurrentShift } from "@/features/shift/api/use-shift";
import {
  useSellableMenu,
  useAddDraftItem,
  useUpdateDraftItemQuantity,
  useUpdateDraftItemSize,
  useUpdateDraftItemModifiers,
  useUpdateDraftItemPreparationNote,
  useRemoveDraftItem,
} from "../api/use-pos";
import { usePosSession } from "../api/use-pos-session";
import { useCheckoutFlow } from "../api/use-checkout";
import { MenuGrid } from "./menu-grid";
import { DraftPanel } from "./draft-panel";
import { CheckPanel } from "./check-panel";
import { PaymentDialog } from "./payment-dialog";
import { ItemPickerDialog, type ItemPickerConfig } from "./item-picker-dialog";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@/components/ui/resizable";
import { matchesDraftItemConfig, diffDraftItemEdits } from "../utils/selection";
import { derivePosPhase, selectOpenCheck, isPostPaymentPhase } from "../utils/phase";
import { calculateDraftSubtotal } from "../utils/pricing";
import type {
  CatalogSellableItemResponse,
  SalesDraftItemResponse,
} from "@/api/generated/models";
import { messageForError } from "@/lib/error-messages";

export function PosView() {
  const { data: shift, isLoading: isShiftLoading } = useCurrentShift();
  const {
    data: menu,
    isLoading: isMenuLoading,
    isError: isMenuError,
    error: menuError,
    refetch: refetchMenu,
  } = useSellableMenu();

  const isShiftOpen = shift?.state === "OPEN";

  const { activeSessionId, session, ensureSessionId, clearSession } = usePosSession();

  // Mutations
  const { addDraftItem, isPending: isAddingItem } = useAddDraftItem(activeSessionId ?? "");
  const { updateQuantity } = useUpdateDraftItemQuantity(activeSessionId ?? "");
  const { updateSize } = useUpdateDraftItemSize(activeSessionId ?? "");
  const { updateModifiers } = useUpdateDraftItemModifiers(activeSessionId ?? "");
  const { updatePreparationNote } = useUpdateDraftItemPreparationNote(activeSessionId ?? "");
  const { removeDraftItem } = useRemoveDraftItem(activeSessionId ?? "");

  // Modal State
  const [pickerItem, setPickerItem] = React.useState<CatalogSellableItemResponse | null>(null);
  const [editingDraftItemId, setEditingDraftItemId] = React.useState<string | null>(null);
  const [pickerInitialValues, setPickerInitialValues] = React.useState<Partial<ItemPickerConfig> | undefined>(undefined);
  const [isPickerOpen, setIsPickerOpen] = React.useState(false);
  const [isSubmittingPicker, setIsSubmittingPicker] = React.useState(false);
  const [errorMessage, setErrorMessage] = React.useState<string | null>(null);

  // Where the sale stands is the server projection, never local state.
  const phase = derivePosPhase(session);
  const draftItems = session?.draft?.items ?? [];
  const openCheck = selectOpenCheck(session);
  const paymentTotal =
    phase === "AWAITING_PAYMENT"
      ? (openCheck?.balance_vnd ?? 0)
      : calculateDraftSubtotal(draftItems);

  const checkout = useCheckoutFlow({
    activeSessionId,
    session,
    phase,
    isShiftOpen,
    draftItemCount: draftItems.length,
    clearSession,
    onDraftError: setErrorMessage,
  });

  // Add Item Handler
  const handleSelectItem = async (item: CatalogSellableItemResponse) => {
    if (!isShiftOpen || !item.id) return;
    setErrorMessage(null);

    const hasSizes = Boolean(item.sizes && item.sizes.length > 0);
    const hasModifiers = Boolean(item.modifier_groups && item.modifier_groups.length > 0);

    // Simple item: 1-tap direct add
    if (!hasSizes && !hasModifiers) {
      try {
        const sid = await ensureSessionId();
        await addDraftItem(
          {
            menu_item_id: item.id,
          },
          undefined,
          sid,
        );
      } catch (err) {
        setErrorMessage(messageForError(err));
      }
      return;
    }

    // Configurable item: open modal
    setPickerItem(item);
    setEditingDraftItemId(null);
    setPickerInitialValues(undefined);
    setIsPickerOpen(true);
  };

  // Edit Item Handler
  const handleEditDraftItem = (draftItem: SalesDraftItemResponse) => {
    if (!isShiftOpen) return;
    setErrorMessage(null);

    // Locate sellable item from menu categories
    let foundCatalogItem: CatalogSellableItemResponse | null = null;
    for (const cat of menu?.categories ?? []) {
      for (const it of cat.items ?? []) {
        if (it.id === draftItem.menu_item_id) {
          foundCatalogItem = it;
          break;
        }
      }
      if (foundCatalogItem) break;
    }

    if (!foundCatalogItem) return;

    setPickerItem(foundCatalogItem);
    setEditingDraftItemId(draftItem.id ?? null);
    setPickerInitialValues({
      sizeId: draftItem.size_id,
      selectedOptionIds: (draftItem.selected_modifier_options ?? [])
        .map((opt) => opt.id)
        .filter(Boolean) as string[],
      preparationNote: draftItem.preparation_note ?? "",
      quantity: draftItem.quantity ?? 1,
    });
    setIsPickerOpen(true);
  };

  // Confirm Modal Handler (Add or Update)
  const handleConfirmPicker = async (config: ItemPickerConfig) => {
    if (!pickerItem?.id) return;
    setErrorMessage(null);
    setIsSubmittingPicker(true);

    try {
      if (editingDraftItemId) {
        // Edit existing draft item: apply updates only for attributes that changed
        const diff = diffDraftItemEdits(pickerInitialValues, config);

        // 1. Update quantity if changed
        if (diff.quantityChanged && config.quantity !== undefined) {
          await updateQuantity(editingDraftItemId, { quantity: config.quantity });
        }

        // 2. Update modifiers if changed
        if (diff.modifiersChanged) {
          await updateModifiers(editingDraftItemId, {
            modifier_option_ids: config.selectedOptionIds,
          });
        }

        // 3. Update preparation note if changed
        if (diff.noteChanged) {
          await updatePreparationNote(editingDraftItemId, {
            preparation_note: config.preparationNote,
          });
        }

        // 4. Update size if changed (execute last to avoid backend merge-deletion race)
        if (diff.sizeChanged && config.sizeId) {
          await updateSize(editingDraftItemId, { size_id: config.sizeId });
        }
      } else {
        // Add new draft item
        const prevItem = session?.draft?.items?.find((it) =>
          matchesDraftItemConfig(it, pickerItem.id!, config),
        );
        const sid = await ensureSessionId();
        const updatedSession = await addDraftItem(
          {
            menu_item_id: pickerItem.id,
            size_id: config.sizeId,
            modifier_option_ids: config.selectedOptionIds,
            preparation_note: config.preparationNote || undefined,
          },
          undefined,
          sid,
        );

        // Preflight ruling: if quantity > 1 on add, update quantity so user choice is preserved
        if (config.quantity > 1 && updatedSession?.draft?.items) {
          const targetItem =
            updatedSession.draft.items
              .slice()
              .reverse()
              .find((it) => matchesDraftItemConfig(it, pickerItem.id!, config)) ??
            updatedSession.draft.items.slice(-1)[0];

          if (targetItem?.id) {
            const targetQty = (prevItem?.quantity ?? 0) + config.quantity;
            await updateQuantity(
              targetItem.id,
              { quantity: targetQty },
              undefined,
              sid,
            );
          }
        }
      }
      setIsPickerOpen(false);
    } catch (err) {
      setErrorMessage(messageForError(err));
    } finally {
      setIsSubmittingPicker(false);
    }
  };

  // Stepper Quantity Change Handler
  const handleQuantityChange = async (itemId: string, nextQty: number) => {
    setErrorMessage(null);
    try {
      await updateQuantity(itemId, { quantity: nextQty });
    } catch (err) {
      setErrorMessage(messageForError(err));
    }
  };

  // Remove Item Handler
  const handleRemoveItem = async (itemId: string) => {
    setErrorMessage(null);
    try {
      await removeDraftItem(itemId);
    } catch (err) {
      setErrorMessage(messageForError(err));
    }
  };

  useHotkeys(
    "f9",
    (event) => {
      event.preventDefault();
      // The item picker is a configuration-in-progress: opening payment over it
      // would commit the draft without the line the cashier is still building.
      if (checkout.isPaymentOpen || isPickerOpen) return;
      if (isPostPaymentPhase(phase)) checkout.nextCustomer();
      else checkout.openPaymentDialog();
    },
    { enableOnFormTags: true },
  );

  if (isShiftLoading || isMenuLoading) {
    return (
      <div className="flex flex-col items-center justify-center p-12 min-h-[60vh] gap-4">
        <div className="w-12 h-12 rounded-2xl bg-primary/10 text-primary flex items-center justify-center animate-pulse">
          <Clock className="w-6 h-6 animate-spin" />
        </div>
        <p className="text-sm text-muted-foreground font-medium">Đang tải thực đơn bán hàng...</p>
      </div>
    );
  }

  if (isMenuError) {
    return (
      <div className="flex flex-col items-center justify-center p-8 max-w-md mx-auto min-h-[60vh] text-center gap-4">
        <div className="w-12 h-12 rounded-2xl bg-destructive/10 text-destructive flex items-center justify-center">
          <AlertCircle className="w-6 h-6" />
        </div>
        <div className="space-y-1">
          <h3 className="text-base font-bold text-foreground">Không thể tải thực đơn</h3>
          <p className="text-xs text-muted-foreground">{messageForError(menuError)}</p>
        </div>
        <Button
          type="button"
          variant="outline"
          onClick={() => refetchMenu()}
          className="rounded-xl h-10 min-h-[48px] px-6"
        >
          <RefreshCw className="w-4 h-4 mr-2" />
          Thử lại
        </Button>
      </div>
    );
  }

  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      <ResizablePanelGroup
        orientation="horizontal"
        className="flex-1 overflow-hidden"
      >
        {/* Zone 1: Menu Grid */}
        <ResizablePanel
          defaultSize="80%"
          minSize="40%"
          className="flex flex-col min-w-[320px] overflow-hidden"
        >
          <MenuGrid
            categories={menu?.categories}
            onSelectItem={handleSelectItem}
            disabled={!isShiftOpen || (phase !== "NO_SESSION" && phase !== "DRAFTING")}
          />
        </ResizablePanel>

        <ResizableHandle withHandle />

        {/* Zone 2: Order Bill Aside */}
        <ResizablePanel
          defaultSize="20%"
          minSize="280px"
          maxSize="60%"
          className="flex flex-col overflow-hidden"
        >
          {phase === "AWAITING_PAYMENT" || isPostPaymentPhase(phase) ? (
            <CheckPanel
              session={session}
              phase={phase}
              onCollect={checkout.openPaymentDialog}
              onSubmit={checkout.submitOrder}
              onClose={() => {}}
              isSubmitting={checkout.submitStatus === "submitting"}
              isClosing={false}
              submitError={checkout.submitError}
              onNextCustomer={checkout.nextCustomer}
              className="w-full h-full flex-1"
            />
          ) : (
            <DraftPanel
              session={session}
              isShiftOpen={isShiftOpen}
              onEditItem={handleEditDraftItem}
              onQuantityChange={handleQuantityChange}
              onRemoveItem={handleRemoveItem}
              onCheckout={checkout.openPaymentDialog}
              canCheckout={isShiftOpen && draftItems.length > 0}
              className="w-full h-full flex-1"
            />
          )}
        </ResizablePanel>
      </ResizablePanelGroup>

      {/* Item Configuration Modal */}
      <ItemPickerDialog
        item={pickerItem}
        initialValues={pickerInitialValues}
        isOpen={isPickerOpen}
        onClose={() => setIsPickerOpen(false)}
        onConfirm={handleConfirmPicker}
        isSubmitting={isAddingItem || isSubmittingPicker}
        confirmLabel={editingDraftItemId ? "Cập nhật món" : "Thêm vào đơn"}
      />

      {/* Cash Payment Modal */}
      <PaymentDialog
        isOpen={checkout.isPaymentOpen}
        serviceNumber={session?.service_number}
        totalVnd={paymentTotal}
        isCommitted={phase === "AWAITING_PAYMENT"}
        isSubmitting={checkout.isPaying}
        errorMessage={checkout.paymentError}
        changeDueVnd={checkout.changeDueVnd}
        submitStatus={checkout.submitStatus}
        submitError={checkout.submitError}
        onClose={checkout.closePaymentDialog}
        onConfirm={checkout.confirmPayment}
        onDone={checkout.finishPayment}
      />

      {/* Global Error Toast Bar if mutation fails */}
      {errorMessage && (
        <div
          role="alert"
          className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 flex items-center gap-2 rounded-xl bg-destructive text-destructive-foreground px-4 py-2.5 text-xs font-bold shadow-lg animate-in fade-in slide-in-from-bottom-2"
        >
          <AlertCircle className="h-4 w-4 shrink-0" />
          <span>{errorMessage}</span>
          <button
            type="button"
            onClick={() => setErrorMessage(null)}
            className="ml-2 text-destructive-foreground/80 hover:text-destructive-foreground underline min-h-[48px] px-2 flex items-center"
          >
            Đóng
          </button>
        </div>
      )}
    </div>
  );
}
