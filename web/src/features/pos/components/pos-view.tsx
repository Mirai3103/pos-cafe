import * as React from "react";
import { useNavigate } from "@tanstack/react-router";
import { useCurrentShift } from "@/features/shift";
import { ErrorToast } from "@/components/feedback/error-toast";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@/components/ui/resizable";
import { messageForError } from "@/lib/error-messages";
import { useSellableMenu } from "../api/use-pos";
import { usePosSession } from "../api/use-pos-session";
import { useCheckoutFlow } from "../api/use-checkout";
import { useCloseFlow } from "../api/use-close-session";
import { useDineInFlow } from "../api/use-dine-in";
import { useChangeTables } from "../hooks/use-change-tables";
import { useDraftEditor } from "../hooks/use-draft-editor";
import { usePendingOrders } from "../hooks/use-pending-orders";
import { usePosHotkeys } from "../hooks/use-pos-hotkeys";
import { deriveDineInStatus, isDineIn as isDineInSession } from "../lib/dine-in";
import { derivePosPhase } from "../lib/phase";
import { MenuGrid } from "./menu-grid";
import { ChangeTablesDialog } from "./change-tables-dialog";
import { CompletedSaleDialog } from "./completed-sale-dialog";
import { ItemPickerDialog } from "./item-picker-dialog";
import { PendingOrdersDrawer, PendingOrdersButton } from "./pending-orders-drawer";
import { PosMenuLoading, PosMenuError } from "./pos-menu-status";
import { PosOrderPanel } from "./pos-order-panel";
import { PosPaymentDialog } from "./pos-payment-dialog";

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

  const [errorMessage, setErrorMessage] = React.useState<string | null>(null);
  const pendingOrders = usePendingOrders();
  const changeTables = useChangeTables(activeSessionId);

  // Where the sale stands is the server projection, never local state.
  const phase = derivePosPhase(session);
  const navigate = useNavigate();
  const isDineIn = isDineInSession(session);
  const dineInStatus = deriveDineInStatus(session);
  const dineIn = useDineInFlow({ activeSessionId, status: dineInStatus, onDraftError: setErrorMessage });

  const editor = useDraftEditor({
    activeSessionId,
    session,
    categories: menu?.categories,
    isShiftOpen,
    ensureSessionId,
    ensureDraft,
    setErrorMessage,
  });

  const checkout = useCheckoutFlow({
    activeSessionId,
    session,
    phase,
    isShiftOpen,
    draftItemCount: session?.draft?.items?.length ?? 0,
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

  const leaveToFloor = () => {
    clearSession();
    void navigate({ to: "/tables" });
  };

  const handleSelectPendingOrder = (sessionId: string) => {
    pendingOrders.close();
    setErrorMessage(null);
    switchSession(sessionId);
  };

  usePosHotkeys({
    phase,
    blocked:
      checkout.isPaymentOpen ||
      dineIn.isPaymentOpen ||
      changeTables.isOpen ||
      editor.isPickerOpen ||
      closeFlow.completedSale !== null,
    isDrawerOpen: pendingOrders.isOpen,
    onCheckout: checkout.openPaymentDialog,
    onSubmit: checkout.submitOrder,
    onNextCustomer: checkout.nextCustomer,
    onClose: closeFlow.closeSession,
    onToggleDrawer: pendingOrders.toggle,
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
              count={pendingOrders.orders.length}
              readyCount={pendingOrders.readyCount}
              onClick={pendingOrders.open}
            />
          </div>
          <MenuGrid
            categories={menu?.categories}
            onSelectItem={editor.selectItem}
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
          <PosOrderPanel
            session={session}
            phase={phase}
            isShiftOpen={isShiftOpen}
            isDineIn={isDineIn}
            dineInStatus={dineInStatus}
            dineIn={dineIn}
            checkout={checkout}
            closeFlow={closeFlow}
            editor={editor}
            onLeave={leaveToFloor}
            onChangeTables={changeTables.open}
          />
        </ResizablePanel>
      </ResizablePanelGroup>

      {/* Item Configuration Modal */}
      <ItemPickerDialog {...editor.pickerProps} />

      {/* Cash Payment Modal */}
      <PosPaymentDialog
        session={session}
        phase={phase}
        isDineIn={isDineIn}
        checkout={checkout}
        dineIn={dineIn}
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
        session={changeTables.isOpen && isDineIn ? session : null}
        onClose={changeTables.close}
      />

      <PendingOrdersDrawer
        isOpen={pendingOrders.isOpen}
        orders={pendingOrders.orders}
        isLoading={pendingOrders.isLoading}
        errorMessage={pendingOrders.errorMessage}
        activeSessionId={activeSessionId}
        nowMs={pendingOrders.nowMs}
        onSelect={handleSelectPendingOrder}
        onClose={pendingOrders.close}
        onRetry={pendingOrders.retry}
      />

      <ErrorToast message={errorMessage} onDismiss={() => setErrorMessage(null)} />
    </div>
  );
}
