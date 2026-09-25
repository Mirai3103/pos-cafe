import * as React from "react";
import { useNavigate } from "@tanstack/react-router";
import { useCurrentShift } from "@/features/shift/api/use-shift";
import {
  useSellableMenu,
  useActiveSessions,
  useAddDraftItem,
  useUpdateDraftItemQuantity,
  useUpdateDraftItemSize,
  useUpdateDraftItemModifiers,
  useUpdateDraftItemPreparationNote,
  useRemoveDraftItem,
} from "../api/use-pos";
import { usePosSession } from "../api/use-pos-session";
import { useCheckoutFlow } from "../api/use-checkout";
import { useCloseFlow } from "../api/use-close-session";
import { useDineInFlow } from "../api/use-dine-in";
import { usePosHotkeys } from "../hooks/use-pos-hotkeys";
import { deriveDineInStatus, isDineIn as isDineInSession } from "../utils/dine-in";
import { MenuGrid } from "./menu-grid";
import { DraftPanel } from "./draft-panel";
import { CheckPanel } from "./check-panel";
import { DineInPanel } from "./dine-in-panel";
import { ChangeTablesDialog } from "./change-tables-dialog";
import { PaymentDialog } from "./payment-dialog";
import { CompletedSaleDialog } from "./completed-sale-dialog";
import { PendingOrdersDrawer, PendingOrdersButton } from "./pending-orders-drawer";
import { PosMenuLoading, PosMenuError } from "./pos-menu-status";
import { PosErrorToast } from "./pos-error-toast";
import { ItemPickerDialog, type ItemPickerConfig } from "./item-picker-dialog";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@/components/ui/resizable";
import { matchesDraftItemConfig, diffDraftItemEdits } from "../utils/selection";
import { derivePosPhase, selectOpenCheck, isPostPaymentPhase } from "../utils/phase";
import { calculateDraftSubtotal } from "../utils/pricing";
import { toPendingOrders, countReadyToClose } from "../utils/pending-orders";
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

  const { activeSessionId, session, ensureSessionId, clearSession, switchSession, ensureDraft } =
    usePosSession();

  const [isDrawerOpen, setIsDrawerOpen] = React.useState(false);
  const activeSessions = useActiveSessions(isDrawerOpen);
  const pendingOrders = toPendingOrders(activeSessions.data);

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
  const navigate = useNavigate();
  const isDineIn = isDineInSession(session);
  const dineInStatus = deriveDineInStatus(session);
  const dineIn = useDineInFlow({ activeSessionId, status: dineInStatus, onDraftError: setErrorMessage });
  const [isChangingTables, setIsChangingTables] = React.useState(false);

  // Another Session took over this terminal: a dialog left open for the
  // previous party would otherwise keep blocking hotkeys with nothing on
  // screen to explain why.
  React.useEffect(() => {
    // oxlint-disable-next-line react/set-state-in-effect
    setIsChangingTables(false);
  }, [activeSessionId]);

  const leaveToFloor = () => {
    clearSession();
    void navigate({ to: "/tables" });
  };

  const draftItems = session?.draft?.items ?? [];
  const openCheck = selectOpenCheck(session);
  const paymentTotal =
    phase === "AWAITING_PAYMENT" ? (openCheck?.balance_vnd ?? 0) : calculateDraftSubtotal(draftItems);

  const checkout = useCheckoutFlow({
    activeSessionId,
    session,
    phase,
    isShiftOpen,
    draftItemCount: draftItems.length,
    clearSession,
    onDraftError: setErrorMessage,
  });

  const closeFlow = useCloseFlow({
    activeSessionId,
    session,
    clearSession,
    onError: setErrorMessage,
    isReady: isDineIn ? dineInStatus.canClose : undefined,
  });

  const handleSelectPendingOrder = (sessionId: string) => {
    setIsDrawerOpen(false);
    setErrorMessage(null);
    switchSession(sessionId);
  };

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
        await ensureDraft(sid);
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
        await ensureDraft(sid);
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

  usePosHotkeys({
    phase,
    blocked:
      checkout.isPaymentOpen || dineIn.isPaymentOpen || isChangingTables || isPickerOpen || closeFlow.completedSale !== null,
    isDrawerOpen,
    onCheckout: checkout.openPaymentDialog,
    onSubmit: checkout.submitOrder,
    onNextCustomer: checkout.nextCustomer,
    onClose: closeFlow.closeSession,
    onToggleDrawer: () => setIsDrawerOpen((open) => !open),
    dineIn: isDineIn
      ? { status: dineInStatus, onSend: dineIn.sendToBar, onCollect: dineIn.openPaymentDialog, onClose: closeFlow.closeSession }
      : undefined,
  });

  if (isShiftLoading || isMenuLoading) return <PosMenuLoading />;

  // A failed 30s poll must not blank the whole POS (menu, draft, open
  // payment dialog): react-query keeps the last successful data alongside
  // isError, so the full-screen failure is reserved for a genuine first-load
  // error (e.g. the inactivity lock or a network blip mid-shift).
  if (isMenuError && !menu) {
    return <PosMenuError message={messageForError(menuError)} onRetry={() => refetchMenu()} />;
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
          <div className="flex items-center justify-end border-b border-border px-3 py-2 shrink-0">
            <PendingOrdersButton
              count={pendingOrders.length}
              readyCount={countReadyToClose(pendingOrders)}
              onClick={() => setIsDrawerOpen(true)}
            />
          </div>
          <MenuGrid
            categories={menu?.categories}
            onSelectItem={handleSelectItem}
            disabled={
              !isShiftOpen ||
              (isDineIn ? !dineInStatus.canOrder : phase !== "NO_SESSION" && phase !== "DRAFTING")
            }
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
          {isDineIn && session ? (
            <DineInPanel
              session={session}
              status={dineInStatus}
              isShiftOpen={isShiftOpen}
              sendError={dineIn.sendError}
              isSending={dineIn.isSending}
              isClosing={closeFlow.isClosing}
              onEditItem={handleEditDraftItem}
              onQuantityChange={handleQuantityChange}
              onRemoveItem={handleRemoveItem}
              onSend={() => void dineIn.sendToBar()}
              onCollect={dineIn.openPaymentDialog}
              onClose={() => void closeFlow.closeSession()}
              onLeave={leaveToFloor}
              onChangeTables={() => setIsChangingTables(true)}
              className="w-full h-full flex-1"
            />
          ) : phase === "AWAITING_PAYMENT" || isPostPaymentPhase(phase) ? (
            <CheckPanel
              session={session}
              phase={phase}
              onCollect={checkout.openPaymentDialog}
              onSubmit={() => void checkout.submitOrder()}
              onClose={() => void closeFlow.closeSession()}
              onNextCustomer={checkout.nextCustomer}
              isSubmitting={checkout.submitStatus === "submitting"}
              isClosing={closeFlow.isClosing}
              submitError={checkout.submitError}
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
        isOpen={isDineIn ? dineIn.isPaymentOpen : checkout.isPaymentOpen}
        serviceNumber={session?.service_number}
        modeLabel={isDineIn ? "Tại bàn" : "Đơn mang đi"}
        totalVnd={isDineIn ? dineIn.paymentTotalVnd : paymentTotal}
        isCommitted={isDineIn || phase === "AWAITING_PAYMENT"}
        isSubmitting={isDineIn ? dineIn.isPaying : checkout.isPaying}
        errorMessage={isDineIn ? dineIn.paymentError : checkout.paymentError}
        changeDueVnd={isDineIn ? dineIn.changeDueVnd : checkout.changeDueVnd}
        submitStatus={isDineIn ? "idle" : checkout.submitStatus}
        submitError={isDineIn ? null : checkout.submitError}
        onClose={isDineIn ? dineIn.closePaymentDialog : checkout.closePaymentDialog}
        onConfirm={isDineIn ? dineIn.confirmPayment : checkout.confirmPayment}
        onDone={isDineIn ? dineIn.finishPayment : checkout.finishPayment}
      />

      <CompletedSaleDialog
        sale={closeFlow.completedSale}
        onDone={() => {
          const wasDineIn = isDineIn;
          closeFlow.dismissCompletedSale();
          if (wasDineIn) void navigate({ to: "/tables" });
        }}
      />

      <ChangeTablesDialog
        session={isChangingTables && isDineIn ? session : null}
        onClose={() => setIsChangingTables(false)}
      />

      <PendingOrdersDrawer
        isOpen={isDrawerOpen}
        orders={pendingOrders}
        isLoading={activeSessions.isLoading}
        errorMessage={activeSessions.isError ? messageForError(activeSessions.error) : null}
        activeSessionId={activeSessionId}
        nowMs={activeSessions.dataUpdatedAt}
        onSelect={handleSelectPendingOrder}
        onClose={() => setIsDrawerOpen(false)}
        onRetry={() => void activeSessions.refetch()}
      />

      <PosErrorToast message={errorMessage} onDismiss={() => setErrorMessage(null)} />
    </div>
  );
}
