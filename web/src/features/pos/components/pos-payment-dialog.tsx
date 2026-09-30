import type { SalesServiceSessionResponse } from "@/api/generated/models";
import type { CheckoutFlow } from "../api/use-checkout";
import type { DineInFlow } from "../api/use-dine-in";
import { selectOpenCheck, type PosPhase } from "../lib/phase";
import { calculateDraftSubtotal } from "../lib/pricing";
import { PaymentDialog } from "./payment-dialog";

export interface PosPaymentDialogProps {
  session: SalesServiceSessionResponse | null;
  phase: PosPhase;
  isDineIn: boolean;
  checkout: CheckoutFlow;
  dineIn: DineInFlow;
}

/**
 * The cash payment dialog, wired to whichever flow owns the current Session:
 * a seated party's bill (dine-in) or the takeaway checkout.
 */
export function PosPaymentDialog({ session, phase, isDineIn, checkout, dineIn }: PosPaymentDialogProps) {
  if (isDineIn) {
    return (
      <PaymentDialog
        isOpen={dineIn.isPaymentOpen}
        serviceNumber={session?.service_number}
        modeLabel="Tại bàn"
        totalVnd={dineIn.paymentTotalVnd}
        isCommitted
        isSubmitting={dineIn.isPaying}
        errorMessage={dineIn.paymentError}
        changeDueVnd={dineIn.changeDueVnd}
        submitStatus="idle"
        submitError={null}
        onClose={dineIn.closePaymentDialog}
        onConfirm={dineIn.confirmPayment}
        onDone={dineIn.finishPayment}
      />
    );
  }

  const isCommitted = phase === "AWAITING_PAYMENT";
  const totalVnd = isCommitted
    ? (selectOpenCheck(session)?.balance_vnd ?? 0)
    : calculateDraftSubtotal(session?.draft?.items ?? []);

  return (
    <PaymentDialog
      isOpen={checkout.isPaymentOpen}
      serviceNumber={session?.service_number}
      modeLabel="Đơn mang đi"
      totalVnd={totalVnd}
      isCommitted={isCommitted}
      isSubmitting={checkout.isPaying}
      errorMessage={checkout.paymentError}
      changeDueVnd={checkout.changeDueVnd}
      submitStatus={checkout.submitStatus}
      submitError={checkout.submitError}
      onClose={checkout.closePaymentDialog}
      onConfirm={checkout.confirmPayment}
      onDone={checkout.finishPayment}
    />
  );
}
