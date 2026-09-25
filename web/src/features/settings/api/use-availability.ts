import { useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import {
  getGetCatalogMenuAvailabilityQueryKey,
  getGetCatalogMenuSellableQueryKey,
  useGetCatalogMenuAvailability,
  usePatchCatalogItemsItemIdAvailability,
  usePatchCatalogModifierOptionsOptionIdAvailability,
  usePatchCatalogSizesSizeIdAvailability,
  usePostCatalogAvailabilityBatch,
} from "@/api/generated/endpoints/catalog/catalog";
import type {
  CatalogAvailabilityMenuResponse,
  GetCatalogMenuAvailability200,
} from "@/api/generated/models";
import { newRequestId } from "@/lib/command";
import { ApiError, unwrap } from "@/lib/unwrap";
import {
  applyAvailability,
  toRestoreChanges,
  type AvailabilityKind,
  type AvailabilityRef,
} from "../lib/availability";

/** Another terminal may change availability; there is no push channel. */
export const AVAILABILITY_POLL_MS = 15_000;

export const STALE_RESTORE_MESSAGE = "Danh sách đã thay đổi, vui lòng kiểm tra lại";

export function useAvailabilityMenu(): UseQueryResult<CatalogAvailabilityMenuResponse, ApiError> {
  return useGetCatalogMenuAvailability<CatalogAvailabilityMenuResponse, ApiError>({
    query: { select: unwrap, refetchInterval: AVAILABILITY_POLL_MS },
  });
}

function useInvalidateMenus() {
  const queryClient = useQueryClient();
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: getGetCatalogMenuAvailabilityQueryKey() }),
      queryClient.invalidateQueries({ queryKey: getGetCatalogMenuSellableQueryKey() }),
    ]).then(() => undefined);
}

/** One toggle is one intent: a fresh request_id, optimistic, rolled back on refusal. */
export function useSetAvailability() {
  const queryClient = useQueryClient();
  const invalidateMenus = useInvalidateMenus();
  const item = usePatchCatalogItemsItemIdAvailability();
  const size = usePatchCatalogSizesSizeIdAvailability();
  const option = usePatchCatalogModifierOptionsOptionIdAvailability();
  const key = getGetCatalogMenuAvailabilityQueryKey();

  return {
    setAvailability: async (kind: AvailabilityKind, id: string, available: boolean) => {
      const data = { request_id: newRequestId(), available };
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<GetCatalogMenuAvailability200>(key);
      queryClient.setQueryData<GetCatalogMenuAvailability200>(key, (old) =>
        old?.data ? { ...old, data: applyAvailability(old.data, kind, id, available) } : old,
      );
      try {
        if (kind === "item") unwrap(await item.mutateAsync({ itemId: id, data }));
        else if (kind === "size") unwrap(await size.mutateAsync({ sizeId: id, data }));
        else unwrap(await option.mutateAsync({ optionId: id, data }));
      } catch (err) {
        queryClient.setQueryData(key, previous);
        throw err;
      } finally {
        void invalidateMenus();
      }
    },
  };
}

export function classifyRestoreFailure(err: unknown): "stale" | "retry" {
  if (err instanceof ApiError && (err.status === 404 || err.status === 409)) return "stale";
  return "retry";
}

export function useRestoreAvailability() {
  const queryClient = useQueryClient();
  const invalidateMenus = useInvalidateMenus();
  const batch = usePostCatalogAvailabilityBatch();

  return {
    isPending: batch.isPending,
    restore: async (refs: AvailabilityRef[], requestId: string) => {
      unwrap(await batch.mutateAsync({ data: { request_id: requestId, changes: toRestoreChanges(refs) } }));
      await invalidateMenus();
    },
    refresh: () => queryClient.refetchQueries({ queryKey: getGetCatalogMenuAvailabilityQueryKey() }),
  };
}
