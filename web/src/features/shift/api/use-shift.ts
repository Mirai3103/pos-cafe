import { useQueryClient } from "@tanstack/react-query";
import {
  useGetShiftsCurrent,
  usePostShifts,
  usePostShiftsShiftIdCashMovements,
  usePostShiftsShiftIdReconciliation,
  usePostShiftsShiftIdReconciliationCashCounts,
  usePostShiftsShiftIdReconciliationQrObservations,
  usePostShiftsShiftIdClose,
  getGetShiftsCurrentQueryKey,
} from "@/api/generated/endpoints/shifts/shifts";
import { unwrap, unwrapNullable } from "@/lib/unwrap";
import type {
  ShiftOpenShiftCommand,
  ShiftRecordCashMovementCommand,
  ShiftStartReconciliationCommand,
  ShiftRecordCashCountCommand,
  ShiftRecordQRObservationCommand,
  ShiftCloseShiftCommand,
} from "@/api/generated/models";

export function useCurrentShift() {
  return useGetShiftsCurrent({
    query: {
      select: unwrapNullable,
      staleTime: 15_000,
    },
  });
}

export function useOpenShift() {
  const queryClient = useQueryClient();
  const mutation = usePostShifts({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetShiftsCurrentQueryKey() });
      },
    },
  });

  return {
    ...mutation,
    openShift: async (command: ShiftOpenShiftCommand) => {
      const res = await mutation.mutateAsync({ data: command });
      return unwrap(res);
    },
  };
}

export function useRecordCashMovement(shiftId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostShiftsShiftIdCashMovements({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetShiftsCurrentQueryKey() });
      },
    },
  });

  return {
    ...mutation,
    recordCashMovement: async (command: ShiftRecordCashMovementCommand) => {
      const res = await mutation.mutateAsync({ shiftId, data: command });
      return unwrap(res);
    },
  };
}

export function useStartReconciliation(shiftId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostShiftsShiftIdReconciliation({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetShiftsCurrentQueryKey() });
      },
    },
  });

  return {
    ...mutation,
    startReconciliation: async (command: ShiftStartReconciliationCommand) => {
      const res = await mutation.mutateAsync({ shiftId, data: command });
      return unwrap(res);
    },
  };
}

export function useRecordCashCount(shiftId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostShiftsShiftIdReconciliationCashCounts({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetShiftsCurrentQueryKey() });
      },
    },
  });

  return {
    ...mutation,
    recordCashCount: async (command: ShiftRecordCashCountCommand) => {
      const res = await mutation.mutateAsync({ shiftId, data: command });
      return unwrap(res);
    },
  };
}

export function useRecordQRObservation(shiftId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostShiftsShiftIdReconciliationQrObservations({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetShiftsCurrentQueryKey() });
      },
    },
  });

  return {
    ...mutation,
    recordQRObservation: async (command: ShiftRecordQRObservationCommand) => {
      const res = await mutation.mutateAsync({ shiftId, data: command });
      return unwrap(res);
    },
  };
}

export function useCloseShift(shiftId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostShiftsShiftIdClose({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetShiftsCurrentQueryKey() });
      },
    },
  });

  return {
    ...mutation,
    closeShift: async (command: ShiftCloseShiftCommand) => {
      const res = await mutation.mutateAsync({ shiftId, data: command });
      return unwrap(res);
    },
  };
}
