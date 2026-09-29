// web/src/features/catalog/api/use-catalog-admin.ts
import { useCallback, useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  getGetCatalogMenuAvailabilityQueryKey,
  getGetCatalogMenuManageQueryKey,
  getGetCatalogMenuSellableQueryKey,
  getGetCatalogModifierGroupsQueryKey,
  useGetCatalogMenuManage,
  useGetCatalogModifierGroups,
} from "@/api/generated/endpoints/catalog/catalog";
import type {
  CatalogManagementMenuResponse,
  CatalogManagementModifierGroupResponse,
  GetCatalogModifierGroups200,
} from "@/api/generated/models";
import { playErrorBuzz, playSuccessChirp } from "@/lib/sound";
import { ApiError, unwrap, unwrapNullable } from "@/lib/unwrap";
import { useManagerApprovalStore } from "@/stores/use-manager-approval-store";
import { toCatalogModel, type CatalogModel, type Retirement } from "../lib/catalog-model";
import { runPlan, type RunOutcome, type StepResult } from "../lib/run-plan";
import { planNeedsPin, planRetire, type RetireKind, type Step } from "../lib/save-plan";
import { executeCommand } from "./execute-command";

export interface CatalogQuery {
  model: CatalogModel;
  isPending: boolean;
  error: ApiError | null;
  refetch: () => void;
}

/** A Go nil slice arrives as `data: null`, so the group list tolerates it. */
const selectGroups = (res: GetCatalogModifierGroups200) => unwrapNullable(res) ?? [];

export function useCatalogModel(): CatalogQuery {
  const menu = useGetCatalogMenuManage<CatalogManagementMenuResponse, ApiError>({ query: { select: unwrap } });
  const groups = useGetCatalogModifierGroups<CatalogManagementModifierGroupResponse[], ApiError>({
    query: { select: selectGroups },
  });
  const model = useMemo(() => toCatalogModel(menu.data, groups.data), [menu.data, groups.data]);
  return {
    model,
    isPending: menu.isPending || groups.isPending,
    error: (!menu.data ? menu.error : null) ?? (!groups.data ? groups.error : null),
    refetch: () => {
      void menu.refetch();
      void groups.refetch();
    },
  };
}

/** Every screen that shows catalog data: this tab, the POS menu, and the 9a tab. */
export function useInvalidateCatalog(): () => Promise<void> {
  const queryClient = useQueryClient();
  return useCallback(async () => {
    await Promise.all(
      [
        getGetCatalogMenuManageQueryKey(),
        getGetCatalogModifierGroupsQueryKey(),
        getGetCatalogMenuSellableQueryKey(),
        getGetCatalogMenuAvailabilityQueryKey(),
      ].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
    );
  }, [queryClient]);
}

export type SaveOutcome = RunOutcome & { cancelled: boolean };

const EMPTY_OUTCOME: RunOutcome = { ok: true, results: [], created: { optionIds: {} } };

/**
 * Runs a planned save (ADR-062): asks for the Manager PIN once when any step
 * needs it, runs the steps in order, then refetches so a retry re-plans
 * against fresh state. The PIN lives only in this call.
 */
export function useCatalogSave() {
  const invalidate = useInvalidateCatalog();
  const [isSaving, setIsSaving] = useState(false);
  const [results, setResults] = useState<StepResult[] | null>(null);

  const save = useCallback(
    async (steps: Step[]): Promise<SaveOutcome> => {
      if (steps.length === 0) return { ...EMPTY_OUTCOME, cancelled: false };
      let pin: string | null = null;
      if (planNeedsPin(steps)) {
        try {
          const creds = await useManagerApprovalStore.getState().promptApproval({
            title: "Xác nhận thay đổi giá",
            description: "Thay đổi này có giá bán. Nhập PIN Quản lý để lưu.",
            confirmLabel: "Xác nhận & Lưu",
          });
          pin = creds.managerPin;
        } catch {
          return { ...EMPTY_OUTCOME, ok: false, cancelled: true };
        }
      }
      setIsSaving(true);
      setResults(null);
      try {
        const outcome = await runPlan(steps, executeCommand, pin);
        setResults(outcome.results);
        if (outcome.ok) playSuccessChirp();
        else playErrorBuzz();
        return { ...outcome, cancelled: false };
      } finally {
        await invalidate();
        setIsSaving(false);
      }
    },
    [invalidate],
  );

  return { save, isSaving, results, clearResults: useCallback(() => setResults(null), []) };
}

/** Retires an item, category, or group; resolves to an error message or null. */
export function useRetireEntity() {
  const { save } = useCatalogSave();
  return useCallback(
    async (kind: RetireKind, id: string, name: string, retirement: Retirement): Promise<string | null> => {
      const outcome = await save(planRetire(kind, id, name, retirement));
      if (outcome.ok) return null;
      return outcome.results.find((r) => r.status === "failed")?.error ?? "Không thể ngừng bán";
    },
    [save],
  );
}
