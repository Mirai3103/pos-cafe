import type {
  SalesCheckResponse,
  SalesServiceSessionResponse,
} from "@/api/generated/models";
import {
  derivePosPhase,
  hasMultipleOpenChecks,
  hasUnsubmittedWork,
  listLiveChecks,
  preparationProgress,
  selectOpenCheck,
  type PosPhase,
  type PreparationProgress,
} from "./phase";

type MaybeSession = SalesServiceSessionResponse | null | undefined;

/**
 * Where a dine-in Session stands.
 *
 * A linear phase cannot describe one: a seated party can at once owe money,
 * wait for drinks, and be ordering its next round. So these are independent
 * facts, each derived from the server projection, and the action bar shows
 * every action whose fact holds.
 */
export interface DineInStatus {
  draftItemCount: number;
  hasEditableDraft: boolean;
  hasUnsubmittedWork: boolean;
  openCheck: SalesCheckResponse | null;
  hasMultipleOpenChecks: boolean;
  progress: PreparationProgress;
  /** Mirrors EvaluateClosureReadiness; the server stays the authority. */
  canClose: boolean;
  /** The server refuses a new round while a committed one is unsent. */
  canOrder: boolean;
}

export function isDineIn(session: MaybeSession): boolean {
  return session?.service_mode === "DINE_IN";
}

export function deriveDineInStatus(session: MaybeSession): DineInStatus {
  const hasEditableDraft = session?.draft?.state === "EDITABLE";
  const draftItemCount = hasEditableDraft ? (session?.draft?.items?.length ?? 0) : 0;
  const unsubmitted = hasUnsubmittedWork(session);
  const checks = listLiveChecks(session);
  const progress = preparationProgress(session);

  const allSettled = checks.length > 0 && checks.every((check) => check.state === "SETTLED");
  const noPendingRefund = (session?.checks ?? []).every(
    (check) => (check.pending_refund_vnd ?? 0) === 0,
  );
  const hasOrder = (session?.orders?.length ?? 0) > 0;

  return {
    draftItemCount,
    hasEditableDraft,
    hasUnsubmittedWork: unsubmitted,
    openCheck: selectOpenCheck(session),
    hasMultipleOpenChecks: hasMultipleOpenChecks(session),
    progress,
    canClose:
      allSettled && noPendingRefund && hasOrder && !unsubmitted && progress.done === progress.total,
    canOrder: !unsubmitted,
  };
}

/**
 * The one label a list row can show, reusing the takeaway phase names so the
 * pending-orders drawer labels and colours both modes alike. First match wins.
 */
export function dineInPhase(status: DineInStatus): PosPhase {
  if (status.canClose) return "READY_TO_CLOSE";
  if (status.hasUnsubmittedWork || status.draftItemCount > 0) return "AWAITING_SUBMIT";
  if (status.openCheck) return "AWAITING_PAYMENT";
  if (status.progress.done < status.progress.total) return "IN_PREPARATION";
  return "DRAFTING";
}

/** A drafted round commits then submits; a round whose commit landed only submits. */
export function planSendToBar(status: DineInStatus): { commit: boolean; submit: boolean } {
  const commit = status.draftItemCount > 0;
  return { commit, submit: commit || status.hasUnsubmittedWork };
}

/**
 * Uncommitted items have no frozen price, so the bill is collected only once
 * the current round is sent or cleared.
 */
export function canCollect(status: DineInStatus): boolean {
  return status.openCheck !== null && !status.hasMultipleOpenChecks && status.draftItemCount === 0;
}

export type DineInF9Action = "none" | "send" | "collect" | "close";

/** F9 fires the first available of Gửi bếp → Thu tiền → Hoàn tất. */
export function resolveDineInF9(status: DineInStatus, blocked: boolean): DineInF9Action {
  if (blocked) return "none";
  if (planSendToBar(status).submit) return "send";
  if (canCollect(status)) return "collect";
  if (status.canClose) return "close";
  return "none";
}

/** Adding an item to a dine-in Session between rounds first opens the next draft. */
export function needsNewRound(session: MaybeSession): boolean {
  if (!session || !isDineIn(session)) return false;
  return session.draft?.state !== "EDITABLE" && !hasUnsubmittedWork(session);
}

/** The bar has work in hand: keep the Session fresh while the terminal shows it. */
export function shouldPollSession(session: MaybeSession): boolean {
  if (isDineIn(session)) {
    const progress = preparationProgress(session);
    return progress.done < progress.total;
  }
  return derivePosPhase(session) === "IN_PREPARATION";
}

export function sessionTableLabel(session: MaybeSession): string {
  const names = (session?.tables ?? []).map((table) => table.name ?? "").filter(Boolean);
  return names.length > 0 ? names.join(", ") : "Chưa có bàn";
}
