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

  const [isSending, setIsSending] = React.useState(false);
  const [sendError, setSendError] = React.useState<string | null>(null);
  const [isPaymentOpen, setIsPaymentOpen] = React.useState(false);
  const [isPaying, setIsPaying] = React.useState(false);
  const [paymentError, setPaymentError] = React.useState<string | null>(null);
  const [changeDueVnd, setChangeDueVnd] = React.useState<number | null>(null);
  // Captured on open: once paid the Check settles and openCheck becomes null,
  // but the change screen must still show what was collected.
  const [dialogTotalVnd, setDialogTotalVnd] = React.useState(0);

  // Another Session carries other intents.
  React.useEffect(() => {
    commitRequestIdRef.current = null;
    submitRequestIdRef.current = null;
    payRequestIdRef.current = null;
    // oxlint-disable-next-line react/set-state-in-effect
    setSendError(null);
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
    payRequestIdRef.current = payRequestIdRef.current ?? newRequestId();
    setDialogTotalVnd(status.openCheck?.balance_vnd ?? 0);
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
    if (!activeSessionId || !check?.id) return;
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
