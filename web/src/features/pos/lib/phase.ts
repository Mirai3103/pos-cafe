import type {
  SalesCheckResponse,
  SalesPreparationUnitResponse,
  SalesServiceSessionResponse,
} from "@/api/generated/models";

/**
 * Where a sale stands, derived from the server's projection.
 *
 * There is deliberately no client-held state machine. Every Sales endpoint
 * returns the whole ServiceSessionResponse, and LoadServiceSession reads the
 * draft through GetEditableDraft, so `draft` is null the moment the draft is
 * committed. Reloading the page, opening a second tab, or recovering from a
 * half-finished checkout all resolve to the same phase for free.
 */
export type PosPhase =
  | "NO_SESSION"
  | "DRAFTING"
  | "AWAITING_PAYMENT"
  | "AWAITING_SUBMIT"
  | "IN_PREPARATION"
  | "READY_TO_CLOSE";

/**
 * Where the automatic submit after payment stands. The checkout flow holds it
 * only to word the payment result screen; no phase decision reads it.
 */
export type SubmitStatus = "idle" | "submitting" | "submitted" | "failed";

type MaybeSession = SalesServiceSessionResponse | null | undefined;

const CHECK_OPEN = "OPEN";
const CHECK_MERGED = "MERGED";

/** Checks that still represent money, with merged-away ones dropped. */
export function listLiveChecks(session: MaybeSession): SalesCheckResponse[] {
  return (session?.checks ?? []).filter(
    (check) => check.state !== CHECK_MERGED && !check.merged_into_check_id,
  );
}

/**
 * The Check to collect against: the oldest OPEN one.
 *
 * An OPEN Check always carries a positive balance — settlement happens in the
 * same transaction that brings the balance to zero — so state alone decides.
 */
export function selectOpenCheck(session: MaybeSession): SalesCheckResponse | null {
  const open = listLiveChecks(session).filter((check) => check.state === CHECK_OPEN);
  if (open.length === 0) return null;

  return open.reduce((oldest, candidate) => {
    const oldestAt = oldest.created_at ?? "";
    const candidateAt = candidate.created_at ?? "";
    return candidateAt !== "" && (oldestAt === "" || candidateAt < oldestAt)
      ? candidate
      : oldest;
  });
}

/**
 * More than one open Check, which only split or merge can produce. Neither
 * ships in this slice, so the Check panel warns and refuses to collect rather
 * than guessing which bill the customer is paying.
 */
export function hasMultipleOpenChecks(session: MaybeSession): boolean {
  return listLiveChecks(session).filter((check) => check.state === CHECK_OPEN).length > 1;
}

/** Locates one Check inside a freshly returned projection. */
export function findCheckById(
  session: MaybeSession,
  checkId: string,
): SalesCheckResponse | null {
  return listLiveChecks(session).find((check) => check.id === checkId) ?? null;
}

const TERMINAL_UNIT_STATES = new Set(["FULFILLED", "CANCELLED", "WASTED"]);

/** A unit the kitchen will not touch again. A Remake is a separate, new unit. */
export function isTerminalUnit(unit: SalesPreparationUnitResponse): boolean {
  return TERMINAL_UNIT_STATES.has(unit.state ?? "");
}

export interface PreparationProgress {
  done: number;
  total: number;
}

export function preparationProgress(session: MaybeSession): PreparationProgress {
  const units = session?.preparation_units ?? [];
  return { done: units.filter(isTerminalUnit).length, total: units.length };
}

/** A committed item the bar has not been told about yet. */
export function hasUnsubmittedWork(session: MaybeSession): boolean {
  return listLiveChecks(session).some((check) =>
    (check.allocations ?? []).some((allocation) => allocation.submitted !== true),
  );
}

const POST_PAYMENT_PHASES: ReadonlySet<PosPhase> = new Set([
  "AWAITING_SUBMIT",
  "IN_PREPARATION",
  "READY_TO_CLOSE",
]);

/** The money is taken; what remains is the kitchen and the closure. */
export function isPostPaymentPhase(phase: PosPhase): boolean {
  return POST_PAYMENT_PHASES.has(phase);
}

export function derivePosPhase(session: MaybeSession): PosPhase {
  if (!session) return "NO_SESSION";

  if (session.draft && session.draft.state === "EDITABLE") return "DRAFTING";

  const checks = listLiveChecks(session);

  // Unreachable by the domain: a committed draft always charges a Check.
  // Answering NO_SESSION keeps the terminal usable instead of blank.
  if (checks.length === 0) return "NO_SESSION";

  if (selectOpenCheck(session)) return "AWAITING_PAYMENT";

  if (hasUnsubmittedWork(session)) return "AWAITING_SUBMIT";

  const progress = preparationProgress(session);
  if (progress.done < progress.total) return "IN_PREPARATION";

  // Submit always creates units, so "no units" is unreachable by the domain.
  // Closure refuses with ORDER_REQUIRED_FOR_CLOSURE if it ever happens.
  return "READY_TO_CLOSE";
}
