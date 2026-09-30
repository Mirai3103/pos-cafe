import type { SalesServiceSessionResponse } from "@/api/generated/models";
import {
  derivePosPhase,
  listLiveChecks,
  preparationProgress,
  type PosPhase,
  type PreparationProgress,
} from "./phase";
import { calculateDraftSubtotal } from "./pricing";
import { deriveDineInStatus, dineInPhase, isDineIn, sessionTableLabel } from "./dine-in";

/** One row of the "Đơn đang chờ" drawer. */
export interface PendingOrder {
  sessionId: string;
  serviceNumber: string;
  createdAt: string;
  phase: PosPhase;
  progress: PreparationProgress;
  totalVnd: number;
  itemSummary: string;
  tableLabel: string | null;
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

function draftItems(session: SalesServiceSessionResponse) {
  return session.draft?.state === "EDITABLE" ? (session.draft.items ?? []) : [];
}

function itemNames(session: SalesServiceSessionResponse): string[] {
  const committed = listLiveChecks(session).flatMap((check) =>
    (check.allocations ?? []).map((allocation) => allocation.name ?? ""),
  );
  return [...committed, ...draftItems(session).map((item) => item.name ?? "")];
}

/** Committed rounds show what their Checks charge; a draft is priced for display only. */
function totalVnd(session: SalesServiceSessionResponse): number {
  const charged = listLiveChecks(session).reduce((sum, check) => sum + (check.charge_vnd ?? 0), 0);
  return charged + calculateDraftSubtotal(draftItems(session));
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
    .filter((session) => Boolean(session.id))
    .map((session) => ({
      sessionId: session.id!,
      serviceNumber: session.service_number ?? "",
      createdAt: session.created_at ?? "",
      phase: isDineIn(session) ? dineInPhase(deriveDineInStatus(session)) : derivePosPhase(session),
      progress: preparationProgress(session),
      totalVnd: totalVnd(session),
      itemSummary: summarizeItems(itemNames(session)),
      tableLabel: isDineIn(session) ? sessionTableLabel(session) : null,
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
