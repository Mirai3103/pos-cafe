import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  usePostSalesServiceSessionsIdClose,
  getGetSalesServiceSessionsIdQueryKey,
  getGetSalesServiceSessionsQueryKey,
} from "@/api/generated/endpoints/sales/sales";
import { unwrap, isConflictError } from "@/lib/unwrap";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { playSuccessChirp, playErrorBuzz } from "@/lib/sound";
import { useCompletedSale } from "./use-pos";
import { derivePosPhase } from "../utils/phase";
import type {
  SalesCompletedSaleResponse,
  SalesServiceSessionResponse,
} from "@/api/generated/models";

export interface CloseFlowOptions {
  activeSessionId: string | null;
  session: SalesServiceSessionResponse | null;
  clearSession: () => void;
  /** Routes closure refusals to the page-level toast. */
  onError: (message: string) => void;
  /** Overrides the takeaway readiness check; dine-in passes its canClose. */
  isReady?: boolean;
}

export interface CloseFlow {
  isClosing: boolean;
  /** Non-null while the Completed Sale dialog is showing. */
  completedSale: SalesCompletedSaleResponse | null;
  closeSession: () => Promise<void>;
  dismissCompletedSale: () => void;
}

/**
 * The session whose Completed Sale must be read back because it closed
 * somewhere else, for example in a second tab. A sale this terminal closed
 * already arrived in the close response.
 */
export function recoverySessionId(
  session: SalesServiceSessionResponse | null | undefined,
  hasClosedHere: boolean,
): string | null {
  if (hasClosedHere) return null;
  return session?.state === "CLOSED" ? (session.id ?? null) : null;
}

/**
 * Whether closure may be attempted. Dine-in supplies its own readiness: an
 * open empty draft reads as DRAFTING to the takeaway phase, but does not
 * block closure on the server.
 */
export function isCloseReady(
  session: SalesServiceSessionResponse | null | undefined,
  isReady: boolean | undefined,
): boolean {
  return isReady ?? derivePosPhase(session) === "READY_TO_CLOSE";
}

/**
 * Freezes a finished Service Session into its Completed Sale and shows it.
 * Closure is idempotent on the server, so a retry after a lost response
 * returns the same sale.
 */
export function useCloseFlow({
  activeSessionId,
  session,
  clearSession,
  onError,
  isReady,
}: CloseFlowOptions): CloseFlow {
  const queryClient = useQueryClient();
  const mutation = usePostSalesServiceSessionsIdClose();

  const requestIdRef = React.useRef<string | null>(null);
  const [isClosing, setIsClosing] = React.useState(false);
  const [closedSale, setClosedSale] = React.useState<SalesCompletedSaleResponse | null>(null);

  const recoveryId = recoverySessionId(session, closedSale !== null);
  const recovered = useCompletedSale(recoveryId);

  // A closed session whose Completed Sale cannot be read has nothing to show.
  React.useEffect(() => {
    if (recoveryId && recovered.isError) clearSession();
  }, [recoveryId, recovered.isError, clearSession]);

  const closeSession = async () => {
    if (!activeSessionId || isClosing) return;
    if (!isCloseReady(session, isReady)) return;

    requestIdRef.current = requestIdRef.current ?? newRequestId();
    setIsClosing(true);

    try {
      const res = await mutation.mutateAsync({
        id: activeSessionId,
        data: { request_id: requestIdRef.current },
      });
      setClosedSale(unwrap(res));
      requestIdRef.current = null;
      void queryClient.invalidateQueries({ queryKey: getGetSalesServiceSessionsQueryKey() });
      playSuccessChirp();
    } catch (err) {
      playErrorBuzz();
      onError(messageForError(err));
      if (isConflictError(err)) {
        void queryClient.invalidateQueries({
          queryKey: getGetSalesServiceSessionsIdQueryKey(activeSessionId),
        });
      }
    } finally {
      setIsClosing(false);
    }
  };

  const dismissCompletedSale = () => {
    const sid = activeSessionId;
    setClosedSale(null);
    requestIdRef.current = null;
    clearSession();
    if (sid) queryClient.removeQueries({ queryKey: getGetSalesServiceSessionsIdQueryKey(sid) });
  };

  return {
    isClosing,
    completedSale: closedSale ?? recovered.data ?? null,
    closeSession,
    dismissCompletedSale,
  };
}
