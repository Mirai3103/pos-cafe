import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { getGetSalesServiceSessionsIdQueryKey } from "@/api/generated/endpoints/sales/sales";
import { ApiError, isConflictError } from "@/lib/unwrap";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { playErrorBuzz, playSuccessChirp } from "@/lib/sound";
import {
  COMMIT_FAILURE_CODES,
  SUBMIT_ALREADY_DONE_CODES,
  useCommitDraft,
  usePayCash,
  useSubmitOrder,
} from "./use-checkout";
import { canCollect, planSendToBar, type DineInStatus } from "../utils/dine-in";
import { findCheckById } from "../utils/phase";
import { latestPaymentChangeDue } from "../utils/payment";

export function classifySendFailure(err: unknown): "draft" | "done" | "retry" {
  if (err instanceof ApiError && COMMIT_FAILURE_CODES.has(err.code)) return "draft";
  if (err instanceof ApiError && SUBMIT_ALREADY_DONE_CODES.has(err.code)) return "done";
  return "retry";
}

export interface PaymentSnapshot {
  checkId: string;
  balanceVnd: number;
}

/**
 * True when the Check being paid moved since the dialog captured it: a
 * different Check opened, or its balance changed because another terminal
 * sent a new round into the same one (the default CURRENT_UNPAID target).
 * confirmPayment must refuse rather than charge a total the cashier never
 * saw on screen.
 */
export function isPaymentSnapshotStale(
  captured: PaymentSnapshot | null,
  current: { id?: string | null; balance_vnd?: number | null } | null | undefined,
): boolean {
  if (!captured) return false;
  if (!current?.id) return true;
  return current.id !== captured.checkId || (current.balance_vnd ?? 0) !== captured.balanceVnd;
}

/** Shown when confirmPayment catches the Check drifting under the dialog. */
export const STALE_PAYMENT_TOTAL_MESSAGE = "Số tiền cần thu đã thay đổi, vui lòng thử lại.";

/**
 * The payment-dialog fields' values right after a Session switch. Another
 * Session's payment dialog must never carry over: a stale `isPaymentOpen`
 * keeps takeaway hotkeys blocked with nothing on screen to explain why, and a
 * stale `dialogTotalVnd` would show the previous party's total if the dialog
 * reopened before the cashier noticed. The `[activeSessionId]` effect applies
 * this same shape, so a change to one is a change to the other.
 */
export const PAYMENT_DIALOG_RESET_STATE: {
  isPaymentOpen: boolean;
  isPaying: boolean;
  paymentError: string | null;
  changeDueVnd: number | null;
  dialogTotalVnd: number;
} = {
  isPaymentOpen: false,
  isPaying: false,
  paymentError: null,
  changeDueVnd: null,
  dialogTotalVnd: 0,
};

export interface DineInFlowOptions {
  activeSessionId: string | null;
  status: DineInStatus;
  /** Commit revalidation failures belong on the draft, via the page toast. */
  onDraftError: (message: string) => void;
}

export interface DineInFlow {
  isSending: boolean;
  sendError: string | null;
  sendToBar: () => Promise<void>;
  isPaymentOpen: boolean;
  isPaying: boolean;
  paymentError: string | null;
  changeDueVnd: number | null;
  paymentTotalVnd: number;
  openPaymentDialog: () => void;
  closePaymentDialog: () => void;
  confirmPayment: (tenderedVnd: number) => Promise<void>;
  finishPayment: () => void;
}

/**
 * Drives a seated party: each round goes to the bar before any money changes
 * hands, and the bill is collected at the end. Takeaway's useCheckoutFlow is
 * untouched; the two share only the commit, submit, and payment seams.
 */
export function useDineInFlow({ activeSessionId, status, onDraftError }: DineInFlowOptions): DineInFlow {
  const sid = activeSessionId ?? "";
  const queryClient = useQueryClient();
  const { commitDraft } = useCommitDraft(sid);
  const { submitOrder } = useSubmitOrder(sid);
  const { payCash } = usePayCash(sid);

  // One request id per intent, kept across retries so a replay after a lost
  // response reproduces the original outcome instead of duplicating it.
  const commitRequestIdRef = React.useRef<string | null>(null);
  const submitRequestIdRef = React.useRef<string | null>(null);
  const payRequestIdRef = React.useRef<string | null>(null);
  // Which Check payRequestIdRef was minted for; a different Check (a new one,
  // or the same id settled and reopened) needs a fresh id so a stale one
  // can never collide with an unrelated payment.
  const payRequestCheckIdRef = React.useRef<string | null>(null);
  // Captured when the dialog opens, so confirmPayment can catch the Check
  // drifting underneath it instead of silently charging a new total.
  const paymentSnapshotRef = React.useRef<PaymentSnapshot | null>(null);

  const [isSending, setIsSending] = React.useState(false);
  const [sendError, setSendError] = React.useState<string | null>(null);
  const [isPaymentOpen, setIsPaymentOpen] = React.useState(false);
  const [isPaying, setIsPaying] = React.useState(false);
  const [paymentError, setPaymentError] = React.useState<string | null>(null);
  const [changeDueVnd, setChangeDueVnd] = React.useState<number | null>(null);
  // Captured on open: once paid the Check settles and openCheck becomes null,
  // but the change screen must still show what was collected.
  const [dialogTotalVnd, setDialogTotalVnd] = React.useState(0);

  // Another Session carries other intents. The payment dialog belongs to the
  // party that just left: a stale isPaymentOpen would keep hotkeys blocked on
  // the next Session, and a stale dialogTotalVnd would show the previous
  // party's total if the dialog reopened before this ran.
  React.useEffect(() => {
    commitRequestIdRef.current = null;
    submitRequestIdRef.current = null;
    payRequestIdRef.current = null;
    payRequestCheckIdRef.current = null;
    paymentSnapshotRef.current = null;
    // oxlint-disable-next-line react/set-state-in-effect
    setSendError(null);
    // oxlint-disable-next-line react/set-state-in-effect
    setIsPaymentOpen(PAYMENT_DIALOG_RESET_STATE.isPaymentOpen);
    // oxlint-disable-next-line react/set-state-in-effect
    setIsPaying(PAYMENT_DIALOG_RESET_STATE.isPaying);
    // oxlint-disable-next-line react/set-state-in-effect
    setPaymentError(PAYMENT_DIALOG_RESET_STATE.paymentError);
    // oxlint-disable-next-line react/set-state-in-effect
    setChangeDueVnd(PAYMENT_DIALOG_RESET_STATE.changeDueVnd);
    // oxlint-disable-next-line react/set-state-in-effect
    setDialogTotalVnd(PAYMENT_DIALOG_RESET_STATE.dialogTotalVnd);
  }, [activeSessionId]);

  const refetchSession = () =>
    queryClient.invalidateQueries({ queryKey: getGetSalesServiceSessionsIdQueryKey(sid) });

  const sendToBar = async () => {
    if (!activeSessionId || isSending) return;
    const plan = planSendToBar(status);
    if (!plan.submit) return;

    setIsSending(true);
    setSendError(null);
    try {
      if (plan.commit) {
        commitRequestIdRef.current = commitRequestIdRef.current ?? newRequestId();
        await commitDraft(commitRequestIdRef.current, activeSessionId);
        commitRequestIdRef.current = null;
      }
      submitRequestIdRef.current = submitRequestIdRef.current ?? newRequestId();
      try {
        await submitOrder(submitRequestIdRef.current, activeSessionId);
      } catch (err) {
        if (classifySendFailure(err) !== "done") throw err;
        await refetchSession();
      }
      submitRequestIdRef.current = null;
      playSuccessChirp();
    } catch (err) {
      playErrorBuzz();
      const message = messageForError(err);
      if (classifySendFailure(err) === "draft") {
        // The draft is still editable; fixing it is a new intent.
        commitRequestIdRef.current = null;
        onDraftError(message);
      } else {
        // A landed commit shows as unsubmitted work, so the same button
        // retries the submit alone.
        setSendError(message);
        if (isConflictError(err)) void refetchSession();
      }
    } finally {
      setIsSending(false);
    }
  };

  const openPaymentDialog = () => {
    if (!canCollect(status)) return;
    const check = status.openCheck;
    const checkId = check?.id ?? null;
    // A different Check than the one the current request id was minted for
    // needs its own id; reusing it could collide with an unrelated payment.
    if (payRequestCheckIdRef.current !== checkId) {
      payRequestIdRef.current = newRequestId();
      payRequestCheckIdRef.current = checkId;
    } else {
      payRequestIdRef.current = payRequestIdRef.current ?? newRequestId();
    }
    const balanceVnd = check?.balance_vnd ?? 0;
    paymentSnapshotRef.current = checkId ? { checkId, balanceVnd } : null;
    setDialogTotalVnd(balanceVnd);
    setPaymentError(null);
    setChangeDueVnd(null);
    setIsPaymentOpen(true);
  };

  const closePaymentDialog = () => {
    if (isPaying) return;
    setIsPaymentOpen(false);
    setPaymentError(null);
  };

  /** Pays the open Check in full. Never commits: rounds are sent separately. */
  const confirmPayment = async (tenderedVnd: number) => {
    const check = status.openCheck;
    if (!activeSessionId || !check?.id || !canCollect(status)) return;
    if (isPaymentSnapshotStale(paymentSnapshotRef.current, check)) {
      // The Check moved under the dialog: a new one opened, or another
      // terminal sent a round into this one and changed the balance. Refresh
      // what is shown and make the cashier confirm the new total instead of
      // silently charging the one captured when the dialog opened.
      if (payRequestCheckIdRef.current !== check.id) {
        payRequestIdRef.current = newRequestId();
        payRequestCheckIdRef.current = check.id;
      }
      paymentSnapshotRef.current = { checkId: check.id, balanceVnd: check.balance_vnd ?? 0 };
      setDialogTotalVnd(check.balance_vnd ?? 0);
      setPaymentError(STALE_PAYMENT_TOTAL_MESSAGE);
      return;
    }
    setPaymentError(null);
    setIsPaying(true);
    try {
      const paid = await payCash(
        check.id,
        { applied_amount_vnd: check.balance_vnd ?? 0, cash_tendered_vnd: tenderedVnd },
        payRequestIdRef.current ?? undefined,
        activeSessionId,
      );
      setChangeDueVnd(latestPaymentChangeDue(findCheckById(paid, check.id)));
      playSuccessChirp();
    } catch (err) {
      playErrorBuzz();
      setPaymentError(messageForError(err));
      if (isConflictError(err)) void refetchSession();
    } finally {
      setIsPaying(false);
    }
  };

  const finishPayment = () => {
    setIsPaymentOpen(false);
    setChangeDueVnd(null);
    setPaymentError(null);
    payRequestIdRef.current = null;
    payRequestCheckIdRef.current = null;
    paymentSnapshotRef.current = null;
  };

  return {
    isSending,
    sendError,
    sendToBar,
    isPaymentOpen,
    isPaying,
    paymentError,
    changeDueVnd,
    paymentTotalVnd: dialogTotalVnd,
    openPaymentDialog,
    closePaymentDialog,
    confirmPayment,
    finishPayment,
  };
}
