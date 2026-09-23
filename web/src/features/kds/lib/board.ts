import type {
  PreparationQueueUnitResponse,
  PreparationBulkAdvanceOutcome,
} from "@/api/generated/models";

export type ColumnKey = "QUEUED" | "IN_PREPARATION" | "READY";

export const COLUMN_KEYS: ColumnKey[] = ["QUEUED", "IN_PREPARATION", "READY"];

export const COLUMN_LABELS: Record<ColumnKey, string> = {
  QUEUED: "Chờ pha",
  IN_PREPARATION: "Đang pha",
  READY: "Đã xong",
};

/** The unit's next state, one Advance away (internal/preparation/domain.go's advanceChain). */
export const NEXT_STATE: Record<ColumnKey, string> = {
  QUEUED: "IN_PREPARATION",
  IN_PREPARATION: "READY",
  READY: "FULFILLED",
};

/**
 * The one legal Correct-state target reachable from a unit visible in this
 * column — null from Queued, which has no prior state to revert to.
 */
export const CORRECT_TARGET: Record<ColumnKey, string | null> = {
  QUEUED: null,
  IN_PREPARATION: "QUEUED",
  READY: "IN_PREPARATION",
};

export const PRIMARY_ACTION_LABEL: Record<ColumnKey, string> = {
  QUEUED: "Bắt đầu làm",
  IN_PREPARATION: "Xong món",
  READY: "Đã giao khách",
};

export interface BoardUnit {
  id: string;
  itemName: string;
  sizeName: string | null;
  modifierSummary: string | null;
  preparationNote: string | null;
  unitNumber: number;
  state: ColumnKey;
  queuedAt: string;
  inPreparationAt: string | null;
  categoryName: string;
  isRemake: boolean;
}

export interface BoardTicket {
  serviceNumber: string;
  tableNames: string[];
  units: BoardUnit[];
}

export type Board = Record<ColumnKey, BoardTicket[]>;

function isColumnKey(state: string | undefined): state is ColumnKey {
  return state === "QUEUED" || state === "IN_PREPARATION" || state === "READY";
}

function toBoardUnit(unit: PreparationQueueUnitResponse): BoardUnit | null {
  if (!unit.id || !isColumnKey(unit.state)) return null;
  const modifierSummary = (unit.modifiers ?? [])
    .map((modifier) => modifier.option_name)
    .filter((name): name is string => Boolean(name))
    .join(", ");
  return {
    id: unit.id,
    itemName: unit.item_name ?? "",
    sizeName: unit.size_name ?? null,
    modifierSummary: modifierSummary || null,
    preparationNote: unit.preparation_note ?? null,
    unitNumber: unit.unit_number ?? 0,
    state: unit.state,
    queuedAt: unit.queued_at ?? "",
    inPreparationAt: unit.in_preparation_at ?? null,
    categoryName: unit.category_name ?? "",
    isRemake: unit.priority === "REMAKE",
  };
}

/** Every category in the active board — exceptional units remain solely for alerts. */
export function distinctCategories(units: PreparationQueueUnitResponse[] | undefined): string[] {
  const seen = new Set<string>();
  for (const unit of units ?? []) {
    if (isColumnKey(unit.state) && unit.category_name) seen.add(unit.category_name);
  }
  return Array.from(seen).sort((a, b) => a.localeCompare(b, "vi"));
}

/**
 * Groups the queue's active units into three columns, then into ticket cards
 * by service_number within each column — the same service_number can appear
 * as a separate ticket in two columns when its units have split across states
 * (partial handoff). The server already orders `units` by priority lane then
 * queued_at then id, and that order is preserved: no independent sort here.
 */
export function buildBoard(
  units: PreparationQueueUnitResponse[] | undefined,
  categoryFilter: string | null,
): Board {
  const board: Board = { QUEUED: [], IN_PREPARATION: [], READY: [] };
  const ticketsByColumn: Record<ColumnKey, Map<string, BoardTicket>> = {
    QUEUED: new Map(),
    IN_PREPARATION: new Map(),
    READY: new Map(),
  };

  for (const raw of units ?? []) {
    if (categoryFilter && raw.category_name !== categoryFilter) continue;
    const unit = toBoardUnit(raw);
    if (!unit) continue;

    const serviceNumber = raw.service_number ?? "";
    const column = ticketsByColumn[unit.state];
    let ticket = column.get(serviceNumber);
    if (!ticket) {
      ticket = { serviceNumber, tableNames: raw.table_names ?? [], units: [] };
      column.set(serviceNumber, ticket);
      board[unit.state].push(ticket);
    }
    ticket.units.push(unit);
  }

  return board;
}

export function minutesSince(iso: string | null | undefined, nowMs: number): number {
  if (!iso) return 0;
  const at = Date.parse(iso);
  if (Number.isNaN(at)) return 0;
  return Math.max(0, Math.floor((nowMs - at) / 60_000));
}

export function formatElapsed(minutes: number): string {
  return minutes < 1 ? "Vừa xong" : `${minutes} phút`;
}

/** How many of a bulk-advance's per-unit outcomes actually advanced vs. failed. */
export function summarizeBulkOutcomes(
  outcomes: PreparationBulkAdvanceOutcome[],
): { succeeded: number; failed: number } {
  let succeeded = 0;
  let failed = 0;
  for (const outcome of outcomes) {
    if (outcome.status === "FAILED") failed += 1;
    else succeeded += 1;
  }
  return { succeeded, failed };
}
