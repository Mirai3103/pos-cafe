import { useQueryClient } from "@tanstack/react-query";
import { useGetCatalogMenuSellable } from "@/api/generated/endpoints/catalog/catalog";
import {
  useGetSalesServiceSessionsId,
  getGetSalesServiceSessionsIdQueryKey,
  usePostSalesServiceSessionsTakeaway,
  usePostSalesServiceSessionsIdDraftItems,
  usePatchSalesServiceSessionsIdDraftItemsItemIdQuantity,
  usePatchSalesServiceSessionsIdDraftItemsItemIdSize,
  usePatchSalesServiceSessionsIdDraftItemsItemIdModifiers,
  usePatchSalesServiceSessionsIdDraftItemsItemIdPreparationNote,
  useDeleteSalesServiceSessionsIdDraftItemsItemId,
} from "@/api/generated/endpoints/sales/sales";
import { unwrap, unwrapNullable } from "@/lib/unwrap";
import { newRequestId } from "@/lib/command";
import type {
  SalesAddDraftItemCommand,
  SalesSetDraftItemQuantityCommand,
  SalesSetDraftItemSizeCommand,
  SalesSetDraftItemModifiersCommand,
  SalesSetDraftItemNoteCommand,
} from "@/api/generated/models";

/**
 * Reads sellable menu categories and items.
 */
export function useSellableMenu() {
  return useGetCatalogMenuSellable({
    query: {
      select: unwrapNullable,
      staleTime: 60_000,
    },
  });
}

/**
 * Reads single Service Session with its active Order Draft projection.
 */
export function useServiceSession(sessionId: string | null) {
  return useGetSalesServiceSessionsId(sessionId ?? "", {
    query: {
      enabled: Boolean(sessionId),
      select: unwrap,
      staleTime: 5_000,
    },
  });
}

/**
 * Mutation to open an anonymous Takeaway Service Session.
 */
export function useStartTakeawaySession() {
  const queryClient = useQueryClient();
  const mutation = usePostSalesServiceSessionsTakeaway();

  return {
    ...mutation,
    startTakeaway: async (requestId?: string) => {
      const rid = requestId ?? newRequestId();
      const res = await mutation.mutateAsync({
        data: { request_id: rid },
      });
      const data = unwrap(res);
      if (data.id) {
        queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(data.id), res);
      }
      return data;
    },
  };
}

/**
 * Mutation to add an Order Draft item.
 */
export function useAddDraftItem(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostSalesServiceSessionsIdDraftItems();

  return {
    ...mutation,
    addDraftItem: async (
      command: SalesAddDraftItemCommand,
      requestId?: string,
      targetSessionId?: string,
    ) => {
      const sid = targetSessionId || sessionId;
      const rid = requestId ?? command.request_id ?? newRequestId();
      const res = await mutation.mutateAsync({
        id: sid,
        data: { ...command, request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sid), res);
      return data;
    },
  };
}

/**
 * Mutation to update draft item quantity.
 */
export function useUpdateDraftItemQuantity(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePatchSalesServiceSessionsIdDraftItemsItemIdQuantity();

  return {
    ...mutation,
    updateQuantity: async (
      itemId: string,
      command: SalesSetDraftItemQuantityCommand,
      requestId?: string,
      targetSessionId?: string,
    ) => {
      const sid = targetSessionId || sessionId;
      const rid = requestId ?? command.request_id ?? newRequestId();
      const res = await mutation.mutateAsync({
        id: sid,
        itemId,
        data: { ...command, request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sid), res);
      return data;
    },
  };
}

/**
 * Mutation to update draft item size.
 */
export function useUpdateDraftItemSize(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePatchSalesServiceSessionsIdDraftItemsItemIdSize();

  return {
    ...mutation,
    updateSize: async (
      itemId: string,
      command: SalesSetDraftItemSizeCommand,
      requestId?: string,
      targetSessionId?: string,
    ) => {
      const sid = targetSessionId || sessionId;
      const rid = requestId ?? command.request_id ?? newRequestId();
      const res = await mutation.mutateAsync({
        id: sid,
        itemId,
        data: { ...command, request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sid), res);
      return data;
    },
  };
}

/**
 * Mutation to update draft item modifier options.
 */
export function useUpdateDraftItemModifiers(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePatchSalesServiceSessionsIdDraftItemsItemIdModifiers();

  return {
    ...mutation,
    updateModifiers: async (
      itemId: string,
      command: SalesSetDraftItemModifiersCommand,
      requestId?: string,
      targetSessionId?: string,
    ) => {
      const sid = targetSessionId || sessionId;
      const rid = requestId ?? command.request_id ?? newRequestId();
      const res = await mutation.mutateAsync({
        id: sid,
        itemId,
        data: { ...command, request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sid), res);
      return data;
    },
  };
}

/**
 * Mutation to update draft item preparation note.
 */
export function useUpdateDraftItemPreparationNote(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePatchSalesServiceSessionsIdDraftItemsItemIdPreparationNote();

  return {
    ...mutation,
    updatePreparationNote: async (
      itemId: string,
      command: SalesSetDraftItemNoteCommand,
      requestId?: string,
      targetSessionId?: string,
    ) => {
      const sid = targetSessionId || sessionId;
      const rid = requestId ?? command.request_id ?? newRequestId();
      const res = await mutation.mutateAsync({
        id: sid,
        itemId,
        data: { ...command, request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sid), res);
      return data;
    },
  };
}

/**
 * Mutation to remove one draft item.
 */
export function useRemoveDraftItem(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = useDeleteSalesServiceSessionsIdDraftItemsItemId();

  return {
    ...mutation,
    removeDraftItem: async (
      itemId: string,
      requestId?: string,
      targetSessionId?: string,
    ) => {
      const sid = targetSessionId || sessionId;
      const rid = requestId ?? newRequestId();
      const res = await mutation.mutateAsync({
        id: sid,
        itemId,
        data: { request_id: rid },
        params: { request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sid), res);
      return data;
    },
  };
}
