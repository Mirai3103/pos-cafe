import type { SalesServiceSessionResponse } from "@/api/generated/models";
import {
  derivePosPhase,
  listLiveChecks,
  preparationProgress,
  type PosPhase,
  type PreparationProgress,
} from "./phase";
import { calculateDraftSubtotal } from "./pricing";

/** One row of the "Đơn đang chờ" drawer. */
export interface PendingOrder {
  sessionId: string;
  serviceNumber: string;
  createdAt: string;
  phase: PosPhase;
  progress: PreparationProgress;
  totalVnd: number;
  itemSummary: string;
}

export const PENDING_PHASE_LABELS: Record<PosPhase, string> = {
  NO_SESSION: "Đơn trống",
  DRAFTING: "Đang soạn",
  AWAITING_PAYMENT: "Chờ thu tiền",
  AWAITING_SUBMIT: "Chờ gửi bếp",
  IN_PREPARATION: "Đang pha chế",
  READY_TO_CLOSE: "Sẵn sàng hoàn tất",
};

// Work the cashier can finish now first; then money taken that the bar has not heard about.
const PRIORITY: Partial<Record<PosPhase, number>> = { READY_TO_CLOSE: 0, AWAITING_SUBMIT: 1 };
const DEFAULT_PRIORITY = 2;

function isDrafting(session: SalesServiceSessionResponse): boolean {
  return session.draft?.state === "EDITABLE";
}

function itemNames(session: SalesServiceSessionResponse): string[] {
  if (isDrafting(session)) return (session.draft?.items ?? []).map((item) => item.name ?? "");
  return listLiveChecks(session).flatMap((check) =>
    (check.allocations ?? []).map((allocation) => allocation.name ?? ""),
  );
}

/** A draft is priced for display only; a committed session shows what its Checks charge. */
function totalVnd(session: SalesServiceSessionResponse): number {
  if (isDrafting(session)) return calculateDraftSubtotal(session.draft?.items);
  return listLiveChecks(session).reduce((sum, check) => sum + (check.charge_vnd ?? 0), 0);
}

export function summarizeItems(names: string[]): string {
  const named = names.filter(Boolean);
  if (named.length === 0) return "Chưa có món";
  const head = named.slice(0, 2).join(", ");
  return named.length > 2 ? `${head} +${named.length - 2} món` : head;
}

export function toPendingOrders(
  sessions: SalesServiceSessionResponse[] | null | undefined,
): PendingOrder[] {
  return (sessions ?? [])
    .filter((session) => session.service_mode === "TAKEAWAY" && Boolean(session.id))
    .map((session) => ({
      sessionId: session.id!,
      serviceNumber: session.service_number ?? "",
      createdAt: session.created_at ?? "",
      phase: derivePosPhase(session),
      progress: preparationProgress(session),
      totalVnd: totalVnd(session),
      itemSummary: summarizeItems(itemNames(session)),
    }))
    .sort((a, b) => {
      const byPriority =
        (PRIORITY[a.phase] ?? DEFAULT_PRIORITY) - (PRIORITY[b.phase] ?? DEFAULT_PRIORITY);
      return byPriority !== 0 ? byPriority : a.createdAt.localeCompare(b.createdAt);
    });
}

export function countReadyToClose(orders: PendingOrder[]): number {
  return orders.filter((order) => order.phase === "READY_TO_CLOSE").length;
}

export function minutesSince(iso: string, nowMs: number): number {
  const at = Date.parse(iso);
  if (Number.isNaN(at)) return 0;
  return Math.max(0, Math.floor((nowMs - at) / 60_000));
}

export function formatAge(minutes: number): string {
  return minutes < 1 ? "Vừa xong" : `${minutes} phút trước`;
}
