import { beforeEach, describe, expect, it, mock } from "bun:test";

type MutationName = "advance" | "advanceMany" | "waste" | "remake" | "correct" | "acknowledge";

const mutationCalls = {} as Record<MutationName, unknown[]>;
const invalidations: unknown[] = [];
const requestPayloads: Record<string, unknown>[] = [];
let successChirps = 0;
let errorBuzzes = 0;
let rejectedMutation: MutationName | null = null;
let rejection: Error;

const responses: Record<MutationName, unknown> = {
  advance: { id: "unit-1" },
  advanceMany: { outcomes: [{ preparation_unit_id: "unit-1" }] },
  waste: { id: "waste-1" },
  remake: { id: "remake-1" },
  correct: { outcomes: [{ correction_id: "correction-1" }] },
  acknowledge: { id: "alert-1" },
};

function mutation(name: MutationName) {
  return {
    isPending: false,
    mutateAsync: async (variables: any) => {
      mutationCalls[name].push(variables);
      if (rejectedMutation === name) throw rejection;
      if (name === "advanceMany") {
        return {
          success: true,
          data: {
            outcomes: (variables.data.preparation_unit_ids as string[]).map((id) => ({
              preparation_unit_id: id,
              status: "ADVANCED",
            })),
          },
        };
      }
      return { success: true, data: responses[name] };
    },
  };
}

mock.module("@tanstack/react-query", () => ({
  useQueryClient: () => ({
    invalidateQueries: (filters: unknown) => {
      invalidations.push(filters);
      return Promise.resolve();
    },
  }),
}));

mock.module("@/api/generated/endpoints/preparation/preparation", () => ({
  usePostPreparationUnitsUnitIdAdvance: () => mutation("advance"),
  usePostPreparationUnitsAdvanceMany: () => mutation("advanceMany"),
  usePostPreparationUnitsUnitIdWaste: () => mutation("waste"),
  usePostPreparationWastesWasteIdRemake: () => mutation("remake"),
  usePostPreparationUnitsCorrectState: () => mutation("correct"),
  usePostPreparationAlertsAlertIdAcknowledge: () => mutation("acknowledge"),
  getGetPreparationQueueQueryKey: () => ["/preparation/queue"],
}));

mock.module("@/lib/command", () => ({
  withRequestId: (payload: Record<string, unknown>) => {
    requestPayloads.push(payload);
    return { ...payload, request_id: `request-${requestPayloads.length}` };
  },
}));

mock.module("@/lib/sound", () => ({
  playSuccessChirp: () => {
    successChirps += 1;
  },
  playErrorBuzz: () => {
    errorBuzzes += 1;
  },
}));

import { usePreparationActions } from "./use-preparation-actions";

beforeEach(() => {
  for (const name of Object.keys(responses) as MutationName[]) mutationCalls[name] = [];
  invalidations.length = 0;
  requestPayloads.length = 0;
  successChirps = 0;
  errorBuzzes = 0;
  rejectedMutation = null;
  rejection = new Error("server refused action");
});

describe("usePreparationActions", () => {
  it("is exported", () => {
    expect(typeof usePreparationActions).toBe("function");
  });

  it("stamps and runs every action, then chirps and invalidates the queue", async () => {
    const actions = usePreparationActions();

    expect(await actions.advanceUnit("unit-1", "IN_PREPARATION")).toEqual({ id: "unit-1" });
    expect(await actions.advanceMany(["unit-1", "unit-2"], "READY")).toEqual([
      { preparation_unit_id: "unit-1", status: "ADVANCED" },
      { preparation_unit_id: "unit-2", status: "ADVANCED" },
    ]);
    expect(await actions.wasteUnit("unit-1", "SPILLED", "Đổ đồ uống")).toEqual({
      id: "waste-1",
    });
    expect(await actions.remakeWaste("waste-1", "REMAKE", "Làm lại")).toEqual({
      id: "remake-1",
    });
    expect(
      await actions.correctState("unit-1", "QUEUED", "MISTAKE", "1234", "Chọn nhầm"),
    ).toEqual([{ correction_id: "correction-1" }]);
    expect(await actions.acknowledgeAlert("alert-1")).toEqual({ id: "alert-1" });

    expect(requestPayloads).toHaveLength(6);
    expect(mutationCalls.advance[0]).toEqual({
      unitId: "unit-1",
      data: { target_state: "IN_PREPARATION", request_id: "request-1" },
    });
    expect(mutationCalls.advanceMany[0]).toEqual({
      data: {
        preparation_unit_ids: ["unit-1", "unit-2"],
        target_state: "READY",
        request_id: "request-2",
      },
    });
    expect(mutationCalls.waste[0]).toEqual({
      unitId: "unit-1",
      data: { reason: "SPILLED", note: "Đổ đồ uống", request_id: "request-3" },
    });
    expect(mutationCalls.remake[0]).toEqual({
      wasteId: "waste-1",
      data: { reason: "REMAKE", note: "Làm lại", request_id: "request-4" },
    });
    expect(mutationCalls.correct[0]).toEqual({
      data: {
        preparation_unit_ids: ["unit-1"],
        target_state: "QUEUED",
        reason: "MISTAKE",
        note: "Chọn nhầm",
        manager_pin: "1234",
        request_id: "request-5",
      },
    });
    expect(mutationCalls.acknowledge[0]).toEqual({
      alertId: "alert-1",
      data: { request_id: "request-6" },
    });
    expect(successChirps).toBe(6);
    expect(errorBuzzes).toBe(0);
    expect(invalidations).toEqual(
      Array.from({ length: 6 }, () => ({ queryKey: ["/preparation/queue"] })),
    );
  });

  it("chunks a whole-ticket bulk advance into requests of at most 50 ids, each with its own request_id", async () => {
    const actions = usePreparationActions();
    const unitIds = Array.from({ length: 120 }, (_, i) => `unit-${i + 1}`);

    const outcomes = await actions.advanceMany(unitIds, "READY");

    expect(outcomes).toHaveLength(120);
    expect(outcomes[119]).toEqual({ preparation_unit_id: "unit-120", status: "ADVANCED" });
    expect(mutationCalls.advanceMany).toHaveLength(3);
    expect(mutationCalls.advanceMany[0].data.preparation_unit_ids).toHaveLength(50);
    expect(mutationCalls.advanceMany[1].data.preparation_unit_ids).toHaveLength(50);
    expect(mutationCalls.advanceMany[2].data.preparation_unit_ids).toHaveLength(20);
    expect(mutationCalls.advanceMany[2].data.preparation_unit_ids[0]).toBe("unit-101");
    const requestIds = mutationCalls.advanceMany.map(
      (call: any) => call.data.request_id,
    ) as string[];
    expect(new Set(requestIds).size).toBe(3);
    expect(invalidations).toEqual([{ queryKey: ["/preparation/queue"] }]);
    expect(successChirps).toBe(1);
  });

  it("buzzes and rethrows without invalidating when a mutation fails", async () => {
    rejectedMutation = "waste";
    const actions = usePreparationActions();

    await expect(actions.wasteUnit("unit-1", "SPILLED")).rejects.toBe(rejection);

    expect(successChirps).toBe(0);
    expect(errorBuzzes).toBe(1);
    expect(invalidations).toHaveLength(0);
  });
});
