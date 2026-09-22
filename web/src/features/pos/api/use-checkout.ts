import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  usePostSalesServiceSessionsIdDraftCommit,
  usePostSalesChecksCheckIdPaymentsCash,
  getGetSalesServiceSessionsIdQueryKey,
} from "@/api/generated/endpoints/sales/sales";
import { unwrap, ApiError } from "@/lib/unwrap";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { playSuccessChirp, playErrorBuzz } from "@/lib/sound";
import { selectOpenCheck, findCheckById, type PosPhase } from "../utils/phase";
import { latestPaymentChangeDue } from "../utils/payment";
import type {
  SalesPayCashCommand,
  SalesServiceSessionResponse,
} from "@/api/generated/models";

/**
 * Commits the Order Draft: the server revalidates it, freezes prices into
 * immutable Committed Items, and charges a Check.
 *
 * The response carries the new Check with the amount actually owed, which is
 * the only figure the following payment may apply. Client-side pricing is a
 * display estimate and must never reach the wire.
 */
export function useCommitDraft(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostSalesServiceSessionsIdDraftCommit();

  return {
    ...mutation,
    commitDraft: async (requestId?: string, targetSessionId?: string) => {
      const sid = targetSessionId || sessionId;
      const rid = requestId ?? newRequestId();
      const res = await mutation.mutateAsync({ id: sid, data: { request_id: rid } });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sid), res);
      return data;
    },
  };
}

/**
 * Records cash against one Check. The Check settles in the same transaction
 * when the payment brings its balance to zero.
 */
export function usePayCash(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostSalesChecksCheckIdPaymentsCash();

  return {
    ...mutation,
    payCash: async (
      checkId: string,
      command: SalesPayCashCommand,
      requestId?: string,
      targetSessionId?: string,
    ) => {
      const sid = targetSessionId || sessionId;
      const rid = requestId ?? command.request_id ?? newRequestId();
      const res = await mutation.mutateAsync({
        checkId,
        data: { ...command, request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sid), res);
      return data;
    },
  };
}

/** Commit-time revalidation failures that send the cashier back to the draft. */
const COMMIT_FAILURE_CODES = new Set([
  "EMPTY_DRAFT",
  "COMMIT_MENU_ITEM_UNAVAILABLE",
  "COMMIT_MENU_ITEM_RETIRED",
  "COMMIT_SIZE_REQUIRED",
  "COMMIT_SIZE_INVALID",
  "COMMIT_SIZE_UNAVAILABLE",
  "COMMIT_SIZE_RETIRED",
  "COMMIT_MODIFIER_OPTION_INVALID",
  "COMMIT_MODIFIER_OPTION_UNAVAILABLE",
  "COMMIT_MODIFIER_OPTION_RETIRED",
  "COMMIT_MODIFIER_GROUP_INVALID",
  "COMMIT_MODIFIER_GROUP_RETIRED",
]);

export interface CheckoutFlowOptions {
  activeSessionId: string | null;
  session: SalesServiceSessionResponse | null;
  /**
   * Where the sale stands, derived by the caller from the server projection.
   * The hook reads it and never re-derives or caches it — a cached copy would
   * be the client-side state machine this terminal deliberately does without.
   */
  phase: PosPhase;
  isShiftOpen: boolean;
  draftItemCount: number;
  clearSession: () => void;
  /** Routes commit-revalidation failures back to the page-level toast. */
  onDraftError: (message: string) => void;
}

export interface CheckoutFlow {
  isPaymentOpen: boolean;
  isPaying: boolean;
  paymentError: string | null;
  changeDueVnd: number | null;
  openPaymentDialog: () => void;
  closePaymentDialog: () => void;
  confirmPayment: (tenderedVnd: number) => Promise<void>;
  finishPayment: () => void;
  nextCustomer: () => void;
}

/**
 * Drives the cash checkout: open the dialog, commit the draft, take the cash,
 * then hand the terminal back for the next customer.
 */
export function useCheckoutFlow(options: CheckoutFlowOptions): CheckoutFlow {
  const {
    activeSessionId,
    session,
    phase,
    isShiftOpen,
    draftItemCount,
    clearSession,
    onDraftError,
  } = options;

  const { commitDraft } = useCommitDraft(activeSessionId ?? "");
  const { payCash } = usePayCash(activeSessionId ?? "");

  const [isPaymentOpen, setIsPaymentOpen] = React.useState(false);
  const [isPaying, setIsPaying] = React.useState(false);
  const [paymentError, setPaymentError] = React.useState<string | null>(null);
  const [changeDueVnd, setChangeDueVnd] = React.useState<number | null>(null);

  // One request id per intent, retained across retries so a replay after a
  // network failure reproduces the original outcome instead of charging twice.
  const commitRequestIdRef = React.useRef<string | null>(null);
  const payRequestIdRef = React.useRef<string | null>(null);

  const openPaymentDialog = () => {
    if (!isShiftOpen && phase === "DRAFTING") return;
    if (phase !== "DRAFTING" && phase !== "AWAITING_PAYMENT") return;
    if (phase === "DRAFTING" && draftItemCount === 0) return;

    commitRequestIdRef.current = commitRequestIdRef.current ?? newRequestId();
    payRequestIdRef.current = payRequestIdRef.current ?? newRequestId();
    setPaymentError(null);
    setChangeDueVnd(null);
    setIsPaymentOpen(true);
  };

  const closePaymentDialog = () => {
    if (isPaying) return;
    setIsPaymentOpen(false);
    setPaymentError(null);
  };

  /**
   * Commit, then take the cash.
   *
   * The applied amount comes from the commit response, never from the
   * client-side subtotal: commit revalidates and freezes prices, so the Check
   * is the only authority on what is owed.
   */
  const confirmPayment = async (tenderedVnd: number) => {
    if (!activeSessionId) return;
    setPaymentError(null);
    setIsPaying(true);

    try {
      let projection = session;

      if (phase === "DRAFTING") {
        projection = await commitDraft(commitRequestIdRef.current ?? undefined, activeSessionId);
      }

      const check = selectOpenCheck(projection);
      if (!check?.id) {
        throw new ApiError(0, "CHECK_NOT_FOUND", "Không tìm thấy hóa đơn vừa chốt");
      }

      const paid = await payCash(
        check.id,
        {
          applied_amount_vnd: check.balance_vnd ?? 0,
          cash_tendered_vnd: tenderedVnd,
        },
        payRequestIdRef.current ?? undefined,
        activeSessionId,
      );

      setChangeDueVnd(latestPaymentChangeDue(findCheckById(paid, check.id)));
      playSuccessChirp();
    } catch (err) {
      playErrorBuzz();
      const message = messageForError(err);
      // A failed commit leaves the draft editable, so the cashier belongs back
      // on the bill to fix whatever the server rejected.
      if (err instanceof ApiError && COMMIT_FAILURE_CODES.has(err.code)) {
        setIsPaymentOpen(false);
        onDraftError(message);
      } else {
        setPaymentError(message);
      }
    } finally {
      setIsPaying(false);
    }
  };

  const finishPayment = () => {
    setIsPaymentOpen(false);
    setChangeDueVnd(null);
    setPaymentError(null);
    commitRequestIdRef.current = null;
    payRequestIdRef.current = null;
  };

  const nextCustomer = () => {
    commitRequestIdRef.current = null;
    payRequestIdRef.current = null;
    clearSession();
  };

  return {
    isPaymentOpen,
    isPaying,
    paymentError,
    changeDueVnd,
    openPaymentDialog,
    closePaymentDialog,
    confirmPayment,
    finishPayment,
    nextCustomer,
  };
}
