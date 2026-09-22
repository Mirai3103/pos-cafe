import { useQueryClient } from "@tanstack/react-query";
import {
  usePostSalesServiceSessionsIdDraftCommit,
  usePostSalesChecksCheckIdPaymentsCash,
  getGetSalesServiceSessionsIdQueryKey,
} from "@/api/generated/endpoints/sales/sales";
import { unwrap } from "@/lib/unwrap";
import { newRequestId } from "@/lib/command";
import type { SalesPayCashCommand } from "@/api/generated/models";

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
