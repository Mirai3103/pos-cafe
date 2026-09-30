import type { SalesServiceSessionResponse } from "@/api/generated/models";
import type { CheckoutFlow } from "../api/use-checkout";
import type { CloseFlow } from "../api/use-close-session";
import type { DineInFlow } from "../api/use-dine-in";
import type { DraftEditor } from "../hooks/use-draft-editor";
import type { DineInStatus } from "../lib/dine-in";
import { isPostPaymentPhase, type PosPhase } from "../lib/phase";
import { CheckPanel } from "./check-panel";
import { DineInPanel } from "./dine-in-panel";
import { DraftPanel } from "./draft-panel";

export interface PosOrderPanelProps {
  session: SalesServiceSessionResponse | null;
  phase: PosPhase;
  isShiftOpen: boolean;
  isDineIn: boolean;
  dineInStatus: DineInStatus;
  dineIn: DineInFlow;
  checkout: CheckoutFlow;
  closeFlow: CloseFlow;
  editor: DraftEditor;
  onLeave: () => void;
  onChangeTables: () => void;
}

/**
 * The Order Bill aside: a seated party's bill, the committed Check, or the
 * editable draft, depending on the Session and where the sale stands.
 */
export function PosOrderPanel({
  session,
  phase,
  isShiftOpen,
  isDineIn,
  dineInStatus,
  dineIn,
  checkout,
  closeFlow,
  editor,
  onLeave,
  onChangeTables,
}: PosOrderPanelProps) {
  if (isDineIn && session) {
    return (
      <DineInPanel
        session={session}
        status={dineInStatus}
        isShiftOpen={isShiftOpen}
        sendError={dineIn.sendError}
        isSending={dineIn.isSending}
        isClosing={closeFlow.isClosing}
        onEditItem={editor.editDraftItem}
        onQuantityChange={editor.changeQuantity}
        onRemoveItem={editor.removeItem}
        onSend={() => void dineIn.sendToBar()}
        onCollect={dineIn.openPaymentDialog}
        onClose={() => void closeFlow.closeSession()}
        onLeave={onLeave}
        onChangeTables={onChangeTables}
        className="w-full h-full flex-1"
      />
    );
  }

  if (phase === "AWAITING_PAYMENT" || isPostPaymentPhase(phase)) {
    return (
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
    );
  }

  return (
    <DraftPanel
      session={session}
      isShiftOpen={isShiftOpen}
      onEditItem={editor.editDraftItem}
      onQuantityChange={editor.changeQuantity}
      onRemoveItem={editor.removeItem}
      onCheckout={checkout.openPaymentDialog}
      canCheckout={isShiftOpen && (session?.draft?.items ?? []).length > 0}
      className="w-full h-full flex-1"
    />
  );
}
