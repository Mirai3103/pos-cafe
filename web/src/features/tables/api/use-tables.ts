import { useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import {
  useGetTablesOverview,
  getGetTablesOverviewQueryKey,
  usePostTables,
  usePatchTablesTableIdName,
  usePatchTablesTableIdAvailability,
} from "@/api/generated/endpoints/tables/tables";
import {
  usePostSalesServiceSessionsDineIn,
  usePutSalesServiceSessionsIdTables,
  getGetSalesServiceSessionsIdQueryKey,
  getGetSalesServiceSessionsQueryKey,
} from "@/api/generated/endpoints/sales/sales";
import type { TablesTableOverviewRow } from "@/api/generated/models";
import { unwrap, type ApiError } from "@/lib/unwrap";

/** Parties arrive and leave; there is no push channel, so the floor polls. */
export const TABLES_OVERVIEW_POLL_MS = 10_000;

export function useTablesOverview(): UseQueryResult<TablesTableOverviewRow[], ApiError> {
  return useGetTablesOverview<TablesTableOverviewRow[], ApiError>({
    query: { select: unwrap, refetchInterval: TABLES_OVERVIEW_POLL_MS },
  });
}

function useInvalidateFloor() {
  const queryClient = useQueryClient();
  return () => {
    void queryClient.invalidateQueries({ queryKey: getGetTablesOverviewQueryKey() });
    void queryClient.invalidateQueries({ queryKey: getGetSalesServiceSessionsQueryKey() });
  };
}

/** Seats a party: opens a dine-in Service Session at one or more Tables. */
export function useStartDineInSession() {
  const queryClient = useQueryClient();
  const invalidateFloor = useInvalidateFloor();
  const mutation = usePostSalesServiceSessionsDineIn();

  return {
    isPending: mutation.isPending,
    startDineIn: async (tableIds: string[], requestId: string) => {
      const res = await mutation.mutateAsync({ data: { request_id: requestId, table_ids: tableIds } });
      const data = unwrap(res);
      if (data.id) queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(data.id), res);
      invalidateFloor();
      return data;
    },
  };
}

/** Replaces a dine-in Session's Tables: move, add, and release in one command. */
export function useSetSessionTables() {
  const queryClient = useQueryClient();
  const invalidateFloor = useInvalidateFloor();
  const mutation = usePutSalesServiceSessionsIdTables();

  return {
    isPending: mutation.isPending,
    setTables: async (sessionId: string, tableIds: string[], requestId: string) => {
      const res = await mutation.mutateAsync({
        id: sessionId,
        data: { request_id: requestId, table_ids: tableIds },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sessionId), res);
      invalidateFloor();
      return data;
    },
  };
}

/** Table administration, for holders of tables.administer. */
export function useTableAdmin() {
  const invalidateFloor = useInvalidateFloor();
  const create = usePostTables();
  const rename = usePatchTablesTableIdName();
  const availability = usePatchTablesTableIdAvailability();

  return {
    isPending: create.isPending || rename.isPending || availability.isPending,
    createTable: async (name: string, requestId: string) => {
      const data = unwrap(await create.mutateAsync({ data: { request_id: requestId, name } }));
      invalidateFloor();
      return data;
    },
    renameTable: async (tableId: string, name: string, requestId: string) => {
      const data = unwrap(
        await rename.mutateAsync({ tableId, data: { request_id: requestId, name } }),
      );
      invalidateFloor();
      return data;
    },
    setAvailability: async (tableId: string, available: boolean, requestId: string) => {
      const data = unwrap(
        await availability.mutateAsync({ tableId, data: { request_id: requestId, available } }),
      );
      invalidateFloor();
      return data;
    },
  };
}
