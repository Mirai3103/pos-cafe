import { useQueryClient } from "@tanstack/react-query";
import {
  usePostPreparationUnitsUnitIdAdvance,
  usePostPreparationUnitsAdvanceMany,
  usePostPreparationUnitsUnitIdWaste,
  usePostPreparationWastesWasteIdRemake,
  usePostPreparationUnitsCorrectState,
  usePostPreparationAlertsAlertIdAcknowledge,
  getGetPreparationQueueQueryKey,
} from "@/api/generated/endpoints/preparation/preparation";
import { unwrap } from "@/lib/unwrap";
import { withRequestId } from "@/lib/command";
import { playSuccessChirp, playErrorBuzz } from "@/lib/sound";
import type {
  PreparationUnitResponse,
  PreparationBulkAdvanceOutcome,
  PreparationWasteResponse,
  PreparationRemakeResponse,
  PreparationCorrectStateOutcome,
  PreparationAlertResponse,
} from "@/api/generated/models";

export interface PreparationActions {
  isPending: boolean;
  advanceUnit: (unitId: string, targetState: string) => Promise<PreparationUnitResponse>;
  advanceMany: (
    unitIds: string[],
    targetState: string,
  ) => Promise<PreparationBulkAdvanceOutcome[]>;
  wasteUnit: (unitId: string, reason: string, note?: string) => Promise<PreparationWasteResponse>;
  remakeWaste: (wasteId: string, reason: string, note?: string) => Promise<PreparationRemakeResponse>;
  correctState: (
    unitId: string,
    targetState: string,
    reason: string,
    managerPin: string,
    note?: string,
  ) => Promise<PreparationCorrectStateOutcome[]>;
  acknowledgeAlert: (alertId: string) => Promise<PreparationAlertResponse>;
}

/** The backend caps one bulk advance at 50 ids (internal/preparation/bulk_advance.go). */
const BULK_ADVANCE_BATCH_SIZE = 50;

/**
 * Every mutation the KDS screen makes, each stamped with its own request_id
 * and invalidating the queue query on success. Errors are rethrown for the
 * caller to turn into a Vietnamese message via messageForError.
 */
export function usePreparationActions(): PreparationActions {
  const queryClient = useQueryClient();
  const invalidateQueue = () =>
    queryClient.invalidateQueries({ queryKey: getGetPreparationQueueQueryKey() });

  const advanceMutation = usePostPreparationUnitsUnitIdAdvance();
  const advanceManyMutation = usePostPreparationUnitsAdvanceMany();
  const wasteMutation = usePostPreparationUnitsUnitIdWaste();
  const remakeMutation = usePostPreparationWastesWasteIdRemake();
  const correctStateMutation = usePostPreparationUnitsCorrectState();
  const acknowledgeMutation = usePostPreparationAlertsAlertIdAcknowledge();

  const isPending =
    advanceMutation.isPending ||
    advanceManyMutation.isPending ||
    wasteMutation.isPending ||
    remakeMutation.isPending ||
    correctStateMutation.isPending ||
    acknowledgeMutation.isPending;

  async function run<T>(mutate: () => Promise<T>): Promise<T> {
    try {
      const result = await mutate();
      playSuccessChirp();
      void invalidateQueue();
      return result;
    } catch (err) {
      playErrorBuzz();
      throw err;
    }
  }

  return {
    isPending,

    advanceUnit: (unitId, targetState) =>
      run(async () => {
        const res = await advanceMutation.mutateAsync({
          unitId,
          data: withRequestId({ target_state: targetState }),
        });
        return unwrap(res);
      }),

    advanceMany: (unitIds, targetState) =>
      run(async () => {
        // One user intent can exceed the backend's 50-id cap, so it is sent as
        // sequential batches whose per-unit outcomes are merged back together.
        // Each batch carries its own request_id: the server keys idempotency by
        // request_id and rejects reuse for a different payload
        // (internal/preparation/executor.go), so batches cannot share one id.
        const outcomes: PreparationBulkAdvanceOutcome[] = [];
        for (let start = 0; start < unitIds.length; start += BULK_ADVANCE_BATCH_SIZE) {
          const batch = unitIds.slice(start, start + BULK_ADVANCE_BATCH_SIZE);
          const res = await advanceManyMutation.mutateAsync({
            data: withRequestId({ preparation_unit_ids: batch, target_state: targetState }),
          });
          outcomes.push(...(unwrap(res).outcomes ?? []));
        }
        return outcomes;
      }),

    wasteUnit: (unitId, reason, note) =>
      run(async () => {
        const res = await wasteMutation.mutateAsync({
          unitId,
          data: withRequestId({ reason, note }),
        });
        return unwrap(res);
      }),

    remakeWaste: (wasteId, reason, note) =>
      run(async () => {
        const res = await remakeMutation.mutateAsync({
          wasteId,
          data: withRequestId({ reason, note }),
        });
        return unwrap(res);
      }),

    correctState: (unitId, targetState, reason, managerPin, note) =>
      run(async () => {
        const res = await correctStateMutation.mutateAsync({
          data: withRequestId({
            preparation_unit_ids: [unitId],
            target_state: targetState,
            reason,
            note,
            manager_pin: managerPin,
          }),
        });
        return unwrap(res).outcomes ?? [];
      }),

    acknowledgeAlert: (alertId) =>
      run(async () => {
        const res = await acknowledgeMutation.mutateAsync({
          alertId,
          data: withRequestId({}),
        });
        return unwrap(res);
      }),
  };
}
