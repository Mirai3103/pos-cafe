# Web Slice 6: KDS / Preparation Queue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `/kds` placeholder with a real, API-backed Kitchen Display System: a three-column Preparation Queue (Queued / In Preparation / Ready) with ticket cards grouped by `service_number`, per-unit Waste and Manager-approved state-correction, active Preparation Alerts, a recent Waste/Remake activity log, and a data-driven category filter.

**Architecture:** One feature directory, `web/src/features/kds/`, split the same way `features/pos/` is: `lib/board.ts` holds pure grouping/derivation logic with no React or network dependency; `api/` wraps the orval-generated `preparation` endpoints behind `unwrap` and a `usePreparationActions` command layer; `components/` holds presentational pieces composed by `kds-view.tsx`. The queue polls every 5 seconds (reusing the existing `IN_PREPARATION_POLL_MS = 5_000` convention from `features/pos/api/use-pos.ts`); every mutation invalidates the queue query on success rather than touching the cache directly (ADR-053 — server state stays on the server).

**Tech Stack:** React 19, TanStack Router/Query, orval-generated Axios client, Zustand (`useManagerApprovalStore`, already built), Tailwind v4 tokens from `design-system/pos-cafe/DESIGN.md`, `bun test` with `react-dom/server`'s `renderToString` for component tests (no Testing Library, no happy-dom, per the sequence spec's testing strategy).

**Spec:** [`docs/superpowers/specs/2026-09-26-web-slice-6-kds-preparation-queue-design.md`](../specs/2026-09-26-web-slice-6-kds-preparation-queue-design.md)

## Global Constraints

- Every screen state must come from the real `GET /preparation/queue` response — no mock data in the shipped path (sequence spec §3.1).
- Every mutation carries a `request_id` generated once per user intent via `newRequestId()`/`withRequestId()` from `web/src/lib/command.ts`, never regenerated per retry (sequence spec §4.3).
- The route is already guarded: `requireCapability("preparation.operate")` in `web/src/routes/_app/kds.tsx` — do not touch that file, it needs no change.
- Correct-state is Manager-PIN-gated via `useManagerApprovalStore().promptApproval()`, the same primitive `close-shift-dialog.tsx` already uses — `managerPin` goes to the backend as `manager_pin`; `approverLoginCode` is not sent.
- Vietnamese only, all user-facing strings.
- Basic tests only: pure logic (`board.ts`) and thin hook/component-render tests. No end-to-end tests, no integration tests (sequence spec §3.6, §9).
- Cancel (`POST /preparation/units/cancel`) is out of scope for this slice — no Cancel affordance ships.
- Shortage-report and label-reprint prototype buttons render **disabled** with a `title="Chưa hỗ trợ"` tooltip — no backing endpoint exists for either.
- One pull request for the whole slice; the slice ends at a UAT gate, not a self-certified "done".

---

## Task 1: Board — pure grouping and derivation logic

No React, no network. Everything downstream depends on this file's exact export names and types.

**Files:**
- Create: `web/src/features/kds/lib/board.ts`
- Test: `web/src/features/kds/lib/board.test.ts`

**Interfaces:**
- Consumes: `PreparationQueueUnitResponse`, `PreparationBulkAdvanceOutcome` from `@/api/generated/models`.
- Produces:
  - `type ColumnKey = "QUEUED" | "IN_PREPARATION" | "READY"`
  - `const COLUMN_KEYS: ColumnKey[]`
  - `const COLUMN_LABELS: Record<ColumnKey, string>`
  - `const NEXT_STATE: Record<ColumnKey, string>`
  - `const CORRECT_TARGET: Record<ColumnKey, string | null>`
  - `const PRIMARY_ACTION_LABEL: Record<ColumnKey, string>`
  - `interface BoardUnit { id: string; itemName: string; sizeName: string | null; modifierSummary: string | null; preparationNote: string | null; unitNumber: number; state: ColumnKey; queuedAt: string; inPreparationAt: string | null; categoryName: string; isRemake: boolean }`
  - `interface BoardTicket { serviceNumber: string; tableNames: string[]; units: BoardUnit[] }`
  - `type Board = Record<ColumnKey, BoardTicket[]>`
  - `buildBoard(units: PreparationQueueUnitResponse[] | undefined, categoryFilter: string | null): Board`
  - `distinctCategories(units: PreparationQueueUnitResponse[] | undefined): string[]`
  - `minutesSince(iso: string | null | undefined, nowMs: number): number`
  - `formatElapsed(minutes: number): string`
  - `summarizeBulkOutcomes(outcomes: PreparationBulkAdvanceOutcome[]): { succeeded: number; failed: number }`

- [ ] **Step 1: Write the failing test**

Create `web/src/features/kds/lib/board.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import {
  buildBoard,
  distinctCategories,
  minutesSince,
  formatElapsed,
  summarizeBulkOutcomes,
  NEXT_STATE,
  CORRECT_TARGET,
} from "./board";
import type { PreparationQueueUnitResponse, PreparationBulkAdvanceOutcome } from "@/api/generated/models";

function u(overrides: Partial<PreparationQueueUnitResponse>): PreparationQueueUnitResponse {
  return {
    id: "u1",
    item_name: "Cà phê sữa đá",
    state: "QUEUED",
    service_number: "001",
    table_names: [],
    category_name: "Đồ uống",
    queued_at: "2026-09-26T01:00:00Z",
    unit_number: 1,
    priority: "STANDARD",
    modifiers: [],
    ...overrides,
  };
}

describe("buildBoard", () => {
  it("groups units into their state column", () => {
    const board = buildBoard(
      [
        u({ id: "a", state: "QUEUED" }),
        u({ id: "b", state: "IN_PREPARATION" }),
        u({ id: "c", state: "READY" }),
      ],
      null,
    );
    expect(board.QUEUED).toHaveLength(1);
    expect(board.IN_PREPARATION).toHaveLength(1);
    expect(board.READY).toHaveLength(1);
  });

  it("groups units sharing a service_number into one ticket, within a column", () => {
    const board = buildBoard(
      [u({ id: "a", service_number: "010" }), u({ id: "b", service_number: "010" })],
      null,
    );
    expect(board.QUEUED).toHaveLength(1);
    expect(board.QUEUED[0].units.map((x) => x.id)).toEqual(["a", "b"]);
  });

  it("keeps the same service_number in separate tickets across columns", () => {
    const board = buildBoard(
      [
        u({ id: "a", service_number: "010", state: "QUEUED" }),
        u({ id: "b", service_number: "010", state: "IN_PREPARATION" }),
      ],
      null,
    );
    expect(board.QUEUED[0].units.map((x) => x.id)).toEqual(["a"]);
    expect(board.IN_PREPARATION[0].units.map((x) => x.id)).toEqual(["b"]);
  });

  it("ignores units in a terminal or exceptional state", () => {
    const board = buildBoard(
      [u({ id: "a", state: "FULFILLED" }), u({ id: "b", state: "WASTED" }), u({ id: "c", state: "CANCELLED" })],
      null,
    );
    expect(board.QUEUED).toHaveLength(0);
    expect(board.IN_PREPARATION).toHaveLength(0);
    expect(board.READY).toHaveLength(0);
  });

  it("filters by category when given one", () => {
    const board = buildBoard(
      [u({ id: "a", category_name: "Bar" }), u({ id: "b", category_name: "Bánh" })],
      "Bar",
    );
    expect(board.QUEUED.flatMap((t) => t.units.map((x) => x.id))).toEqual(["a"]);
  });

  it("flags a REMAKE-priority unit and ignores STANDARD", () => {
    const board = buildBoard(
      [u({ id: "a", priority: "REMAKE" }), u({ id: "b", priority: "STANDARD" })],
      null,
    );
    const byId = Object.fromEntries(board.QUEUED[0].units.map((x) => [x.id, x]));
    expect(byId.a.isRemake).toBe(true);
    expect(byId.b.isRemake).toBe(false);
  });

  it("joins modifier option names for display", () => {
    const board = buildBoard(
      [u({ id: "a", modifiers: [{ group_name: "Đá", option_name: "Ít đá" }, { group_name: "Đường", option_name: "50%" }] })],
      null,
    );
    expect(board.QUEUED[0].units[0].modifierSummary).toBe("Ít đá, 50%");
  });

  it("tolerates a missing list", () => {
    expect(buildBoard(undefined, null)).toEqual({ QUEUED: [], IN_PREPARATION: [], READY: [] });
  });
});

describe("distinctCategories", () => {
  it("lists each category once, alphabetically", () => {
    expect(distinctCategories([u({ category_name: "Trà" }), u({ category_name: "Bar" }), u({ category_name: "Bar" })])).toEqual([
      "Bar",
      "Trà",
    ]);
  });

  it("tolerates a missing list", () => {
    expect(distinctCategories(undefined)).toEqual([]);
  });
});

describe("elapsed time", () => {
  it("counts whole minutes and never goes negative", () => {
    const now = Date.parse("2026-09-26T01:10:30Z");
    expect(minutesSince("2026-09-26T01:00:00Z", now)).toBe(10);
    expect(minutesSince("2026-09-26T02:00:00Z", now)).toBe(0);
    expect(minutesSince(null, now)).toBe(0);
    expect(minutesSince("garbage", now)).toBe(0);
  });

  it("words the elapsed time", () => {
    expect(formatElapsed(0)).toBe("Vừa xong");
    expect(formatElapsed(7)).toBe("7 phút");
  });
});

describe("state transition tables", () => {
  it("advances one step forward per column", () => {
    expect(NEXT_STATE.QUEUED).toBe("IN_PREPARATION");
    expect(NEXT_STATE.IN_PREPARATION).toBe("READY");
    expect(NEXT_STATE.READY).toBe("FULFILLED");
  });

  it("corrects one step backward, except from Queued", () => {
    expect(CORRECT_TARGET.QUEUED).toBeNull();
    expect(CORRECT_TARGET.IN_PREPARATION).toBe("QUEUED");
    expect(CORRECT_TARGET.READY).toBe("IN_PREPARATION");
  });
});

describe("summarizeBulkOutcomes", () => {
  it("counts advanced and failed outcomes", () => {
    const outcomes: PreparationBulkAdvanceOutcome[] = [
      { preparation_unit_id: "a", status: "ADVANCED" },
      { preparation_unit_id: "b", status: "FAILED", code: "INVALID_TRANSITION" },
      { preparation_unit_id: "c", status: "ADVANCED" },
    ];
    expect(summarizeBulkOutcomes(outcomes)).toEqual({ succeeded: 2, failed: 1 });
  });

  it("tolerates an empty list", () => {
    expect(summarizeBulkOutcomes([])).toEqual({ succeeded: 0, failed: 0 });
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/kds/lib/board.test.ts`
Expected: FAIL — module `./board` not found.

- [ ] **Step 3: Write the implementation**

Create `web/src/features/kds/lib/board.ts`:

```ts
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
    .map((m) => m.option_name)
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

/** Every category actually present in the live queue — never a hardcoded station list. */
export function distinctCategories(units: PreparationQueueUnitResponse[] | undefined): string[] {
  const seen = new Set<string>();
  for (const unit of units ?? []) {
    if (unit.category_name) seen.add(unit.category_name);
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
```

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/features/kds/lib/board.test.ts`
Expected: PASS, all assertions.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/kds/lib/board.ts web/src/features/kds/lib/board.test.ts
git commit -m "feat(web): add pure board grouping logic for the preparation queue"
```

---

## Task 2: Vietnamese error messages and the queue read hook

**Files:**
- Modify: `web/src/lib/error-messages.ts`
- Create: `web/src/features/kds/api/use-preparation-queue.ts`
- Test: `web/src/features/kds/api/use-preparation-queue.test.ts`

**Interfaces:**
- Consumes: `useGetPreparationQueue` from `@/api/generated/endpoints/preparation/preparation`; `unwrap` from `@/lib/unwrap`.
- Produces:
  - `const PREPARATION_QUEUE_POLL_MS = 5_000`
  - `usePreparationQueue(): UseQueryResult<PreparationQueueResponse, ApiError>`
  - New entries in `ERROR_MESSAGES`: `PREPARATION_UNIT_NOT_FOUND`, `INVALID_TRANSITION`, `INVALID_STORED_RESULT`, `PREPARATION_ALERT_NOT_FOUND`, `PREPARATION_WASTE_NOT_FOUND`, `INVALID_PREPARATION_REASON`, `PREPARATION_ALERT_ALREADY_ACKNOWLEDGED`, `PREPARATION_WASTE_ALREADY_REMADE`, `NOT_AUTHORIZED`, `INVALID_INPUT`.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/kds/api/use-preparation-queue.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { usePreparationQueue, PREPARATION_QUEUE_POLL_MS } from "./use-preparation-queue";
import { ERROR_MESSAGES } from "@/lib/error-messages";

describe("usePreparationQueue", () => {
  it("is exported", () => {
    expect(typeof usePreparationQueue).toBe("function");
  });

  it("polls every 5 seconds, matching the existing IN_PREPARATION convention", () => {
    expect(PREPARATION_QUEUE_POLL_MS).toBe(5_000);
  });
});

describe("preparation error messages", () => {
  it("maps every stable preparation error code to Vietnamese", () => {
    for (const code of [
      "PREPARATION_UNIT_NOT_FOUND",
      "INVALID_TRANSITION",
      "INVALID_STORED_RESULT",
      "PREPARATION_ALERT_NOT_FOUND",
      "PREPARATION_WASTE_NOT_FOUND",
      "INVALID_PREPARATION_REASON",
      "PREPARATION_ALERT_ALREADY_ACKNOWLEDGED",
      "PREPARATION_WASTE_ALREADY_REMADE",
      "NOT_AUTHORIZED",
      "INVALID_INPUT",
    ]) {
      expect(ERROR_MESSAGES[code]).toBeTruthy();
    }
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/kds/api/use-preparation-queue.test.ts`
Expected: FAIL — module `./use-preparation-queue` not found.

- [ ] **Step 3: Add the error codes**

In `web/src/lib/error-messages.ts`, add these entries to `ERROR_MESSAGES` (after `COMPLETED_SALE_NOT_FOUND`, before the closing `};`):

```ts
  PREPARATION_UNIT_NOT_FOUND: "Không tìm thấy món cần pha chế.",
  INVALID_TRANSITION: "Món đã chuyển sang trạng thái khác. Vui lòng tải lại màn hình.",
  INVALID_STORED_RESULT: "Yêu cầu trước đó gặp lỗi. Vui lòng thử lại.",
  PREPARATION_ALERT_NOT_FOUND: "Không tìm thấy cảnh báo này.",
  PREPARATION_WASTE_NOT_FOUND: "Không tìm thấy bản ghi huỷ món này.",
  INVALID_PREPARATION_REASON: "Lý do không hợp lệ.",
  PREPARATION_ALERT_ALREADY_ACKNOWLEDGED: "Cảnh báo này đã được xác nhận.",
  PREPARATION_WASTE_ALREADY_REMADE: "Món này đã được pha lại trước đó.",
  NOT_AUTHORIZED: "Mã PIN Quản lý không đúng hoặc không có quyền thực hiện thao tác này.",
  INVALID_INPUT: "Dữ liệu gửi lên không hợp lệ.",
```

- [ ] **Step 4: Write the queue read hook**

Create `web/src/features/kds/api/use-preparation-queue.ts`:

```ts
import { useGetPreparationQueue } from "@/api/generated/endpoints/preparation/preparation";
import { unwrap } from "@/lib/unwrap";

/** The kitchen moves units; there is no push channel, so the board polls. */
export const PREPARATION_QUEUE_POLL_MS = 5_000;

/**
 * Reads the active Preparation Queue: units, alerts, and recent
 * Waste/Remake history in one consistent projection.
 */
export function usePreparationQueue() {
  return useGetPreparationQueue({
    query: {
      select: unwrap,
      refetchInterval: PREPARATION_QUEUE_POLL_MS,
    },
  });
}
```

- [ ] **Step 5: Run the tests**

Run: `cd web && bun test src/features/kds/api/use-preparation-queue.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/error-messages.ts web/src/features/kds/api/use-preparation-queue.ts web/src/features/kds/api/use-preparation-queue.test.ts
git commit -m "feat(web): add preparation queue read hook and error messages"
```

---

## Task 3: Preparation actions hook

Wraps every `/preparation/*` mutation this slice uses behind one hook: `request_id` on every call, sound feedback, and a queue-query invalidation on success — matching `use-close-session.ts`'s `useCloseFlow` shape.

**Files:**
- Create: `web/src/features/kds/api/use-preparation-actions.ts`
- Test: `web/src/features/kds/api/use-preparation-actions.test.ts`

**Interfaces:**
- Consumes: `usePostPreparationUnitsUnitIdAdvance`, `usePostPreparationUnitsAdvanceMany`, `usePostPreparationUnitsUnitIdWaste`, `usePostPreparationWastesWasteIdRemake`, `usePostPreparationUnitsCorrectState`, `usePostPreparationAlertsAlertIdAcknowledge`, `getGetPreparationQueueQueryKey` from `@/api/generated/endpoints/preparation/preparation`; `unwrap` from `@/lib/unwrap`; `withRequestId` from `@/lib/command`; `playSuccessChirp`, `playErrorBuzz` from `@/lib/sound`.
- Produces:

```ts
export interface PreparationActions {
  isPending: boolean;
  advanceUnit: (unitId: string, targetState: string) => Promise<PreparationUnitResponse>;
  advanceMany: (unitIds: string[], targetState: string) => Promise<PreparationBulkAdvanceOutcome[]>;
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
export function usePreparationActions(): PreparationActions;
```

- [ ] **Step 1: Write the failing test**

Create `web/src/features/kds/api/use-preparation-actions.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { usePreparationActions } from "./use-preparation-actions";

describe("usePreparationActions", () => {
  it("is exported", () => {
    expect(typeof usePreparationActions).toBe("function");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/kds/api/use-preparation-actions.test.ts`
Expected: FAIL — module `./use-preparation-actions` not found.

- [ ] **Step 3: Write the implementation**

Create `web/src/features/kds/api/use-preparation-actions.ts`:

```ts
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
  advanceMany: (unitIds: string[], targetState: string) => Promise<PreparationBulkAdvanceOutcome[]>;
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
        const res = await advanceManyMutation.mutateAsync({
          data: withRequestId({ preparation_unit_ids: unitIds, target_state: targetState }),
        });
        return unwrap(res).outcomes ?? [];
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
```

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/features/kds/api/use-preparation-actions.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/kds/api/use-preparation-actions.ts web/src/features/kds/api/use-preparation-actions.test.ts
git commit -m "feat(web): add preparation actions hook for advance, waste, remake, correct-state"
```

---

## Task 4: Ticket card

The core interaction surface: renders one ticket's unit lines, lets the barista check specific units for partial handoff, and exposes Waste and Undo per line.

**Files:**
- Create: `web/src/features/kds/components/ticket-card.tsx`
- Test: `web/src/features/kds/components/ticket-card.test.tsx`

**Interfaces:**
- Consumes: `BoardTicket`, `BoardUnit`, `ColumnKey`, `NEXT_STATE`, `CORRECT_TARGET`, `PRIMARY_ACTION_LABEL`, `minutesSince`, `formatElapsed` (Task 1); `Card`, `CardHeader`, `CardTitle`, `CardContent`, `CardFooter` from `@/components/ui/card`; `Badge` from `@/components/ui/badge`; `Checkbox` from `@/components/ui/checkbox`; `Button` from `@/components/ui/button`.
- Produces:

```ts
export interface TicketCardProps {
  ticket: BoardTicket;
  column: ColumnKey;
  nowMs: number;
  busy: boolean;
  onAdvance: (unitIds: string[], targetState: string) => void;
  onRequestWaste: (unit: BoardUnit) => void;
  onRequestCorrectState: (unit: BoardUnit) => void;
}
export function TicketCard(props: TicketCardProps): React.ReactElement;
```

- [ ] **Step 1: Write the failing test**

Create `web/src/features/kds/components/ticket-card.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TicketCard } from "./ticket-card";
import type { BoardTicket } from "../lib/board";

function ticket(overrides: Partial<BoardTicket> = {}): BoardTicket {
  return {
    serviceNumber: "014",
    tableNames: [],
    units: [
      {
        id: "u1",
        itemName: "Bạc xỉu đá",
        sizeName: "L",
        modifierSummary: "Ít ngọt",
        preparationNote: null,
        unitNumber: 1,
        state: "QUEUED",
        queuedAt: "2026-09-26T01:00:00Z",
        inPreparationAt: null,
        categoryName: "Đồ uống",
        isRemake: false,
      },
    ],
    ...overrides,
  };
}

const base = {
  column: "QUEUED" as const,
  nowMs: Date.parse("2026-09-26T01:03:00Z"),
  busy: false,
  onAdvance: () => {},
  onRequestWaste: () => {},
  onRequestCorrectState: () => {},
};

describe("TicketCard", () => {
  it("shows the ticket's service number and its unit's item name", () => {
    const html = renderToString(<TicketCard {...base} ticket={ticket()} />);
    expect(html).toContain("014");
    expect(html).toContain("Bạc xỉu đá");
  });

  it("shows table names for a dine-in ticket", () => {
    const html = renderToString(<TicketCard {...base} ticket={ticket({ tableNames: ["T1-01"] })} />);
    expect(html).toContain("T1-01");
  });

  it("labels the primary action for its column", () => {
    expect(renderToString(<TicketCard {...base} ticket={ticket()} column="QUEUED" />)).toContain("Bắt đầu làm");
    expect(renderToString(<TicketCard {...base} ticket={ticket()} column="IN_PREPARATION" />)).toContain("Xong món");
    expect(renderToString(<TicketCard {...base} ticket={ticket()} column="READY" />)).toContain("Đã giao khách");
  });

  it("flags a Remake unit", () => {
    const remade = ticket({ units: [{ ...ticket().units[0], isRemake: true }] });
    expect(renderToString(<TicketCard {...base} ticket={remade} />)).toContain("PHA LẠI");
  });

  it("shows the undo action only when the column has a correction target", () => {
    const queued = renderToString(<TicketCard {...base} ticket={ticket()} column="QUEUED" />);
    const inPrep = renderToString(<TicketCard {...base} ticket={ticket()} column="IN_PREPARATION" />);
    expect(queued).not.toContain("Hoàn tác");
    expect(inPrep).toContain("Hoàn tác");
  });

  it("shows the disabled shortage and reprint affordances", () => {
    const html = renderToString(<TicketCard {...base} ticket={ticket()} />);
    expect(html).toContain("Chưa hỗ trợ");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/kds/components/ticket-card.test.tsx`
Expected: FAIL — module `./ticket-card` not found.

- [ ] **Step 3: Write the implementation**

Create `web/src/features/kds/components/ticket-card.tsx`:

```tsx
import { useEffect, useState } from "react";
import { Trash2, Undo2, AlertTriangle, Printer } from "lucide-react";
import { Card, CardHeader, CardTitle, CardContent, CardFooter } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Button } from "@/components/ui/button";
import {
  NEXT_STATE,
  CORRECT_TARGET,
  PRIMARY_ACTION_LABEL,
  minutesSince,
  formatElapsed,
  type BoardTicket,
  type BoardUnit,
  type ColumnKey,
} from "../lib/board";

export interface TicketCardProps {
  ticket: BoardTicket;
  column: ColumnKey;
  nowMs: number;
  busy: boolean;
  onAdvance: (unitIds: string[], targetState: string) => void;
  onRequestWaste: (unit: BoardUnit) => void;
  onRequestCorrectState: (unit: BoardUnit) => void;
}

export function TicketCard({
  ticket,
  column,
  nowMs,
  busy,
  onAdvance,
  onRequestWaste,
  onRequestCorrectState,
}: TicketCardProps) {
  const [selected, setSelected] = useState<Set<string>>(new Set());

  // Drop a selected id once its unit leaves this ticket (advanced, wasted, or
  // corrected away) so a stale checkbox never drives the next tap.
  useEffect(() => {
    setSelected((prev) => {
      const ids = new Set(ticket.units.map((u) => u.id));
      const next = new Set([...prev].filter((id) => ids.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [ticket]);

  const correctTarget = CORRECT_TARGET[column];
  const ageField = column === "QUEUED" ? "queuedAt" : "inPreparationAt";
  const oldestMinutes = ticket.units.reduce(
    (max, unit) => Math.max(max, minutesSince(unit[ageField], nowMs)),
    0,
  );

  function toggle(unitId: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(unitId)) next.delete(unitId);
      else next.add(unitId);
      return next;
    });
  }

  const targetIds = selected.size > 0 ? Array.from(selected) : ticket.units.map((u) => u.id);
  const primaryLabel =
    selected.size > 0 ? `${PRIMARY_ACTION_LABEL[column]} (${selected.size})` : PRIMARY_ACTION_LABEL[column];

  return (
    <Card size="sm" className="border-t-4 border-t-accent">
      <CardHeader className="pb-2 flex flex-row items-center justify-between">
        <CardTitle className="text-sm font-bold">
          Đơn #{ticket.serviceNumber}
          {ticket.tableNames.length > 0 ? ` (${ticket.tableNames.join(", ")})` : ""}
        </CardTitle>
        <Badge variant="accent">{formatElapsed(oldestMinutes)}</Badge>
      </CardHeader>
      <CardContent className="text-sm flex flex-col gap-2">
        {ticket.units.map((unit) => (
          <div key={unit.id} className="flex items-start gap-2 border-b border-border pb-1.5 last:border-0 last:pb-0">
            <Checkbox
              checked={selected.has(unit.id)}
              onCheckedChange={() => toggle(unit.id)}
              className="mt-0.5"
              aria-label={`Chọn ${unit.itemName}`}
            />
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-1.5 flex-wrap">
                <span className="font-medium truncate">{unit.itemName}</span>
                {unit.sizeName && <span className="text-2xs text-muted-foreground">({unit.sizeName})</span>}
                {unit.isRemake && (
                  <Badge variant="destructive" className="text-[10px]">
                    PHA LẠI
                  </Badge>
                )}
              </div>
              {unit.modifierSummary && (
                <p className="text-2xs text-muted-foreground truncate">{unit.modifierSummary}</p>
              )}
              {unit.preparationNote && (
                <p className="text-2xs text-amber-700 truncate">Ghi chú: {unit.preparationNote}</p>
              )}
            </div>
            <span className="font-mono text-2xs text-muted-foreground shrink-0">#{unit.unitNumber}</span>
            <button
              type="button"
              onClick={() => onRequestWaste(unit)}
              disabled={busy}
              title="Huỷ món (lỗi pha chế, không đạt, khách yêu cầu...)"
              className="p-1 rounded text-muted-foreground hover:text-destructive hover:bg-destructive/10 disabled:opacity-50 shrink-0"
            >
              <Trash2 className="w-3.5 h-3.5" />
            </button>
            {correctTarget && (
              <button
                type="button"
                onClick={() => onRequestCorrectState(unit)}
                disabled={busy}
                title="Hoàn tác thao tác gần nhất (cần Quản lý duyệt)"
                className="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-muted disabled:opacity-50 shrink-0"
              >
                <Undo2 className="w-3.5 h-3.5" />
              </button>
            )}
          </div>
        ))}
      </CardContent>
      <CardFooter className="flex items-center gap-2">
        <Button
          type="button"
          disabled={busy}
          onClick={() => onAdvance(targetIds, NEXT_STATE[column])}
          className="flex-1 h-12 rounded-xl font-bold"
        >
          {primaryLabel}
        </Button>
        <button
          type="button"
          disabled
          title="Chưa hỗ trợ"
          className="p-2.5 rounded-xl border border-border text-muted-foreground opacity-50 cursor-not-allowed"
        >
          <AlertTriangle className="w-4 h-4" />
        </button>
        <button
          type="button"
          disabled
          title="Chưa hỗ trợ"
          className="p-2.5 rounded-xl border border-border text-muted-foreground opacity-50 cursor-not-allowed"
        >
          <Printer className="w-4 h-4" />
        </button>
      </CardFooter>
    </Card>
  );
}
```

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/features/kds/components/ticket-card.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/kds/components/ticket-card.tsx web/src/features/kds/components/ticket-card.test.tsx
git commit -m "feat(web): add the KDS ticket card with partial-handoff selection"
```

---

## Task 5: Queue column

**Files:**
- Create: `web/src/features/kds/components/queue-column.tsx`
- Test: `web/src/features/kds/components/queue-column.test.tsx`

**Interfaces:**
- Consumes: `COLUMN_LABELS`, `BoardTicket`, `BoardUnit`, `ColumnKey` (Task 1); `TicketCard` (Task 4).
- Produces:

```ts
export interface QueueColumnProps {
  column: ColumnKey;
  tickets: BoardTicket[];
  nowMs: number;
  busy: boolean;
  onAdvance: (unitIds: string[], targetState: string) => void;
  onRequestWaste: (unit: BoardUnit) => void;
  onRequestCorrectState: (unit: BoardUnit) => void;
}
export function QueueColumn(props: QueueColumnProps): React.ReactElement;
```

- [ ] **Step 1: Write the failing test**

Create `web/src/features/kds/components/queue-column.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { QueueColumn } from "./queue-column";
import type { BoardTicket } from "../lib/board";

const base = {
  column: "QUEUED" as const,
  nowMs: Date.parse("2026-09-26T01:03:00Z"),
  busy: false,
  onAdvance: () => {},
  onRequestWaste: () => {},
  onRequestCorrectState: () => {},
};

const ticket: BoardTicket = {
  serviceNumber: "014",
  tableNames: [],
  units: [
    {
      id: "u1",
      itemName: "Bạc xỉu đá",
      sizeName: null,
      modifierSummary: null,
      preparationNote: null,
      unitNumber: 1,
      state: "QUEUED",
      queuedAt: "2026-09-26T01:00:00Z",
      inPreparationAt: null,
      categoryName: "Đồ uống",
      isRemake: false,
    },
  ],
};

describe("QueueColumn", () => {
  it("shows the column title and unit count", () => {
    const html = renderToString(<QueueColumn {...base} tickets={[ticket]} />);
    expect(html).toContain("Chờ pha");
    expect(html).toContain("014");
  });

  it("says so when the column is empty", () => {
    expect(renderToString(<QueueColumn {...base} tickets={[]} />)).toContain("Không có đơn");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/kds/components/queue-column.test.tsx`
Expected: FAIL — module `./queue-column` not found.

- [ ] **Step 3: Write the implementation**

Create `web/src/features/kds/components/queue-column.tsx`:

```tsx
import { COLUMN_LABELS, type BoardTicket, type BoardUnit, type ColumnKey } from "../lib/board";
import { TicketCard } from "./ticket-card";

export interface QueueColumnProps {
  column: ColumnKey;
  tickets: BoardTicket[];
  nowMs: number;
  busy: boolean;
  onAdvance: (unitIds: string[], targetState: string) => void;
  onRequestWaste: (unit: BoardUnit) => void;
  onRequestCorrectState: (unit: BoardUnit) => void;
}

export function QueueColumn({ column, tickets, nowMs, busy, onAdvance, onRequestWaste, onRequestCorrectState }: QueueColumnProps) {
  const unitCount = tickets.reduce((sum, t) => sum + t.units.length, 0);

  return (
    <div className="flex flex-col gap-3 min-w-0 min-h-0">
      <div className="flex items-center justify-between px-1">
        <h2 className="font-bold text-sm text-foreground">{COLUMN_LABELS[column]}</h2>
        <span className="text-xs font-mono text-muted-foreground">{unitCount}</span>
      </div>
      <div className="flex flex-col gap-3 overflow-y-auto min-h-0">
        {tickets.length === 0 ? (
          <p className="text-xs text-muted-foreground text-center py-6">Không có đơn</p>
        ) : (
          tickets.map((ticket) => (
            <TicketCard
              key={ticket.serviceNumber}
              ticket={ticket}
              column={column}
              nowMs={nowMs}
              busy={busy}
              onAdvance={onAdvance}
              onRequestWaste={onRequestWaste}
              onRequestCorrectState={onRequestCorrectState}
            />
          ))
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/features/kds/components/queue-column.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/kds/components/queue-column.tsx web/src/features/kds/components/queue-column.test.tsx
git commit -m "feat(web): add the KDS queue column"
```

---

## Task 6: Waste dialog

**Files:**
- Create: `web/src/features/kds/components/waste-dialog.tsx`
- Test: `web/src/features/kds/components/waste-dialog.test.tsx`

**Interfaces:**
- Consumes: `usePreparationActions` (Task 3); `messageForError` from `@/lib/error-messages`; `BoardUnit` (Task 1); `Button` from `@/components/ui/button`; `Input` from `@/components/ui/input`.
- Produces:

```ts
export interface WasteDialogProps {
  unit: BoardUnit | null;
  onClose: () => void;
}
export function WasteDialog(props: WasteDialogProps): React.ReactElement | null;
```

Reason catalog (`wasteReasons` in `internal/preparation/domain.go`): `PREPARATION_ERROR`, `QUALITY_FAILURE`, `CUSTOMER_REQUEST`, `OTHER`.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/kds/components/waste-dialog.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { WasteDialog } from "./waste-dialog";

describe("WasteDialog", () => {
  it("renders nothing without a target unit", () => {
    expect(renderToString(<WasteDialog unit={null} onClose={() => {}} />)).toBe("");
  });

  it("shows the unit and every reason option", () => {
    const html = renderToString(
      <WasteDialog
        unit={{
          id: "u1",
          itemName: "Bạc xỉu đá",
          sizeName: null,
          modifierSummary: null,
          preparationNote: null,
          unitNumber: 2,
          state: "IN_PREPARATION",
          queuedAt: "2026-09-26T01:00:00Z",
          inPreparationAt: "2026-09-26T01:02:00Z",
          categoryName: "Đồ uống",
          isRemake: false,
        }}
        onClose={() => {}}
      />,
    );
    expect(html).toContain("Bạc xỉu đá");
    expect(html).toContain("Lỗi pha chế");
    expect(html).toContain("Không đạt chất lượng");
    expect(html).toContain("Khách yêu cầu");
    expect(html).toContain("Khác");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/kds/components/waste-dialog.test.tsx`
Expected: FAIL — module `./waste-dialog` not found.

- [ ] **Step 3: Write the implementation**

Create `web/src/features/kds/components/waste-dialog.tsx`:

```tsx
import { useState } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { usePreparationActions } from "../api/use-preparation-actions";
import { messageForError } from "@/lib/error-messages";
import type { BoardUnit } from "../lib/board";

const WASTE_REASONS = [
  { value: "PREPARATION_ERROR", label: "Lỗi pha chế" },
  { value: "QUALITY_FAILURE", label: "Không đạt chất lượng" },
  { value: "CUSTOMER_REQUEST", label: "Khách yêu cầu" },
  { value: "OTHER", label: "Khác" },
] as const;

export interface WasteDialogProps {
  unit: BoardUnit | null;
  onClose: () => void;
}

export function WasteDialog({ unit, onClose }: WasteDialogProps) {
  const { wasteUnit, isPending } = usePreparationActions();
  const [reason, setReason] = useState<string>(WASTE_REASONS[0].value);
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);

  if (!unit) return null;

  const handleSubmit = async () => {
    if (reason === "OTHER" && !note.trim()) {
      setError("Vui lòng nhập ghi chú khi chọn lý do khác");
      return;
    }
    try {
      await wasteUnit(unit.id, reason, note.trim() || undefined);
      setNote("");
      setReason(WASTE_REASONS[0].value);
      setError(null);
      onClose();
    } catch (err) {
      setError(messageForError(err));
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-sm overflow-hidden flex flex-col p-6 gap-4">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-base font-bold text-foreground">Huỷ món</h3>
            <p className="text-xs text-muted-foreground">
              {unit.itemName} #{unit.unitNumber}
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg text-muted-foreground hover:bg-muted transition"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">Lý do</label>
          <select
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            className="w-full h-9 px-2.5 rounded-lg border border-input bg-background text-xs font-medium"
          >
            {WASTE_REASONS.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label}
              </option>
            ))}
          </select>
        </div>

        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Ghi chú {reason === "OTHER" ? "(bắt buộc)" : "(không bắt buộc)"}
          </label>
          <Input value={note} onChange={(e) => setNote(e.target.value)} placeholder="Mô tả lý do..." />
        </div>

        {error && <p className="text-xs text-destructive text-center font-medium">{error}</p>}

        <div className="flex items-center gap-3 mt-2">
          <Button type="button" variant="outline" onClick={onClose} className="w-1/2 h-12 rounded-xl">
            Huỷ bỏ
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={handleSubmit}
            disabled={isPending}
            className="w-1/2 h-12 rounded-xl font-bold"
          >
            {isPending ? "Đang xử lý..." : "Xác nhận huỷ món"}
          </Button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/features/kds/components/waste-dialog.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/kds/components/waste-dialog.tsx web/src/features/kds/components/waste-dialog.test.tsx
git commit -m "feat(web): add the waste dialog"
```

---

## Task 7: Correct-state dialog (Manager Approval)

**Files:**
- Create: `web/src/features/kds/components/correct-state-dialog.tsx`
- Test: `web/src/features/kds/components/correct-state-dialog.test.tsx`

**Interfaces:**
- Consumes: `usePreparationActions` (Task 3); `useManagerApprovalStore` from `@/stores/use-manager-approval-store`; `messageForError` from `@/lib/error-messages`; `CORRECT_TARGET`, `BoardUnit`, `ColumnKey` (Task 1); `Button` from `@/components/ui/button`; `Input` from `@/components/ui/input`.
- Produces:

```ts
export interface CorrectStateDialogProps {
  unit: BoardUnit | null;
  column: ColumnKey | null;
  onClose: () => void;
}
export function CorrectStateDialog(props: CorrectStateDialogProps): React.ReactElement | null;
```

Reason catalog (`correctionReasons` in `internal/preparation/domain.go`): `STATE_RECORDED_IN_ERROR`, `OTHER`.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/kds/components/correct-state-dialog.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { CorrectStateDialog } from "./correct-state-dialog";

const unit = {
  id: "u1",
  itemName: "Bạc xỉu đá",
  sizeName: null,
  modifierSummary: null,
  preparationNote: null,
  unitNumber: 2,
  state: "IN_PREPARATION" as const,
  queuedAt: "2026-09-26T01:00:00Z",
  inPreparationAt: "2026-09-26T01:02:00Z",
  categoryName: "Đồ uống",
  isRemake: false,
};

describe("CorrectStateDialog", () => {
  it("renders nothing without a target unit", () => {
    expect(renderToString(<CorrectStateDialog unit={null} column={null} onClose={() => {}} />)).toBe("");
  });

  it("renders nothing for Queued, which has no correction target", () => {
    expect(renderToString(<CorrectStateDialog unit={unit} column="QUEUED" onClose={() => {}} />)).toBe("");
  });

  it("shows the unit and both reason options", () => {
    const html = renderToString(<CorrectStateDialog unit={unit} column="IN_PREPARATION" onClose={() => {}} />);
    expect(html).toContain("Bạc xỉu đá");
    expect(html).toContain("Ghi nhận nhầm thao tác");
    expect(html).toContain("Khác");
    expect(html).toContain("Quản lý");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/kds/components/correct-state-dialog.test.tsx`
Expected: FAIL — module `./correct-state-dialog` not found.

- [ ] **Step 3: Write the implementation**

Create `web/src/features/kds/components/correct-state-dialog.tsx`:

```tsx
import { useState } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { usePreparationActions } from "../api/use-preparation-actions";
import { useManagerApprovalStore } from "@/stores/use-manager-approval-store";
import { messageForError } from "@/lib/error-messages";
import { CORRECT_TARGET, type BoardUnit, type ColumnKey } from "../lib/board";

const CORRECTION_REASONS = [
  { value: "STATE_RECORDED_IN_ERROR", label: "Ghi nhận nhầm thao tác" },
  { value: "OTHER", label: "Khác" },
] as const;

export interface CorrectStateDialogProps {
  unit: BoardUnit | null;
  column: ColumnKey | null;
  onClose: () => void;
}

export function CorrectStateDialog({ unit, column, onClose }: CorrectStateDialogProps) {
  const { correctState, isPending } = usePreparationActions();
  const promptApproval = useManagerApprovalStore((s) => s.promptApproval);
  const [reason, setReason] = useState<string>(CORRECTION_REASONS[0].value);
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);

  const targetState = column ? CORRECT_TARGET[column] : null;
  if (!unit || !column || !targetState) return null;

  const handleSubmit = async () => {
    if (reason === "OTHER" && !note.trim()) {
      setError("Vui lòng nhập ghi chú khi chọn lý do khác");
      return;
    }
    try {
      const creds = await promptApproval({
        title: "Duyệt hoàn tác thao tác",
        description: `Hoàn tác "${unit.itemName}" #${unit.unitNumber} về trạng thái trước đó.`,
        confirmLabel: "Xác nhận hoàn tác",
      });
      await correctState(unit.id, targetState, reason, creds.managerPin, note.trim() || undefined);
      setNote("");
      setReason(CORRECTION_REASONS[0].value);
      setError(null);
      onClose();
    } catch (err) {
      if (err instanceof Error && err.message === "MANAGER_APPROVAL_CANCELLED") return;
      setError(messageForError(err));
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-sm overflow-hidden flex flex-col p-6 gap-4">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-base font-bold text-foreground">Hoàn tác thao tác</h3>
            <p className="text-xs text-muted-foreground">
              {unit.itemName} #{unit.unitNumber} — cần Quản lý duyệt
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg text-muted-foreground hover:bg-muted transition"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">Lý do</label>
          <select
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            className="w-full h-9 px-2.5 rounded-lg border border-input bg-background text-xs font-medium"
          >
            {CORRECTION_REASONS.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label}
              </option>
            ))}
          </select>
        </div>

        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Ghi chú {reason === "OTHER" ? "(bắt buộc)" : "(không bắt buộc)"}
          </label>
          <Input value={note} onChange={(e) => setNote(e.target.value)} placeholder="Mô tả lý do..." />
        </div>

        {error && <p className="text-xs text-destructive text-center font-medium">{error}</p>}

        <div className="flex items-center gap-3 mt-2">
          <Button type="button" variant="outline" onClick={onClose} className="w-1/2 h-12 rounded-xl">
            Huỷ bỏ
          </Button>
          <Button
            type="button"
            onClick={handleSubmit}
            disabled={isPending}
            className="w-1/2 h-12 rounded-xl font-bold"
          >
            {isPending ? "Đang xử lý..." : "Yêu cầu Quản lý duyệt"}
          </Button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/features/kds/components/correct-state-dialog.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/kds/components/correct-state-dialog.tsx web/src/features/kds/components/correct-state-dialog.test.tsx
git commit -m "feat(web): add the manager-approved correct-state dialog"
```

---

## Task 8: Alerts panel, corrections log, category filter

Three small presentational pieces. The corrections log's "Pha lại" button passes the whole entry, not just an id, so the caller can default the Remake's reason and note to the original Waste's own — no separate reason picker for Remake.

**Files:**
- Create: `web/src/features/kds/components/alerts-panel.tsx`
- Create: `web/src/features/kds/components/corrections-log.tsx`
- Create: `web/src/features/kds/components/category-filter-bar.tsx`
- Test: `web/src/features/kds/components/alerts-panel.test.tsx`
- Test: `web/src/features/kds/components/corrections-log.test.tsx`
- Test: `web/src/features/kds/components/category-filter-bar.test.tsx`

**Interfaces:**
- Consumes: `PreparationQueueAlertResponse`, `PreparationQueueCorrectionResponse` from `@/api/generated/models`; `Accordion`, `AccordionItem`, `AccordionTrigger`, `AccordionContent` from `@/components/ui/accordion`; `Button` from `@/components/ui/button`; `cn` from `@/lib/utils`.
- Produces:

```ts
export interface AlertsPanelProps {
  alerts: PreparationQueueAlertResponse[];
  busy: boolean;
  onAcknowledge: (alertId: string) => void;
}
export function AlertsPanel(props: AlertsPanelProps): React.ReactElement | null;

export interface CorrectionsLogProps {
  corrections: PreparationQueueCorrectionResponse[];
  busy: boolean;
  onRemake: (entry: PreparationQueueCorrectionResponse) => void;
}
export function CorrectionsLog(props: CorrectionsLogProps): React.ReactElement | null;

export interface CategoryFilterBarProps {
  categories: string[];
  active: string | null;
  onSelect: (category: string | null) => void;
}
export function CategoryFilterBar(props: CategoryFilterBarProps): React.ReactElement | null;
```

- [ ] **Step 1: Write the failing tests**

Create `web/src/features/kds/components/alerts-panel.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { AlertsPanel } from "./alerts-panel";

const base = { busy: false, onAcknowledge: () => {} };

describe("AlertsPanel", () => {
  it("renders nothing when there are no alerts", () => {
    expect(renderToString(<AlertsPanel {...base} alerts={[]} />)).toBe("");
  });

  it("shows each alert's kind, item, unit and service number", () => {
    const html = renderToString(
      <AlertsPanel
        {...base}
        alerts={[
          {
            id: "al1",
            kind: "WASTE",
            item_name: "Bạc xỉu đá",
            unit_number: 2,
            service_number: "014",
            reason: "QUALITY_FAILURE",
          },
        ]}
      />,
    );
    expect(html).toContain("Đã huỷ do lỗi/hết");
    expect(html).toContain("Bạc xỉu đá");
    expect(html).toContain("014");
  });
});
```

Create `web/src/features/kds/components/corrections-log.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { CorrectionsLog } from "./corrections-log";

const base = { busy: false, onRemake: () => {} };

describe("CorrectionsLog", () => {
  it("renders nothing when there are no entries", () => {
    expect(renderToString(<CorrectionsLog {...base} corrections={[]} />)).toBe("");
  });

  it("shows a Waste entry with a Remake action", () => {
    const html = renderToString(
      <CorrectionsLog
        {...base}
        corrections={[
          { id: "w1", entry_kind: "WASTE", item_name: "Trà đào", unit_number: 1, service_number: "020" },
        ]}
      />,
    );
    expect(html).toContain("Trà đào");
    expect(html).toContain("Pha lại");
  });

  it("shows a Remake entry with no Remake action of its own", () => {
    const html = renderToString(
      <CorrectionsLog
        {...base}
        corrections={[
          {
            id: "r1",
            entry_kind: "REMAKE",
            item_name: "Trà đào",
            unit_number: 2,
            service_number: "020",
            waste_id: "w1",
            source_preparation_unit_id: "u1",
            source_unit_number: 1,
          },
        ]}
      />,
    );
    expect(html).not.toContain("<button");
  });
});
```

Create `web/src/features/kds/components/category-filter-bar.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { CategoryFilterBar } from "./category-filter-bar";

describe("CategoryFilterBar", () => {
  it("renders nothing when there are no categories", () => {
    expect(renderToString(<CategoryFilterBar categories={[]} active={null} onSelect={() => {}} />)).toBe("");
  });

  it("shows 'Tất cả' plus every category", () => {
    const html = renderToString(
      <CategoryFilterBar categories={["Bar", "Trà"]} active={null} onSelect={() => {}} />,
    );
    expect(html).toContain("Tất cả");
    expect(html).toContain("Bar");
    expect(html).toContain("Trà");
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun test src/features/kds/components/alerts-panel.test.tsx src/features/kds/components/corrections-log.test.tsx src/features/kds/components/category-filter-bar.test.tsx`
Expected: FAIL — modules not found.

- [ ] **Step 3: Write the implementations**

Create `web/src/features/kds/components/alerts-panel.tsx`:

```tsx
import { AlertTriangle, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { PreparationQueueAlertResponse } from "@/api/generated/models";

const ALERT_KIND_LABELS: Record<string, string> = {
  CANCELLATION: "Đã huỷ",
  CHANGE: "Đã đổi món",
  WASTE: "Đã huỷ do lỗi/hết",
};

export interface AlertsPanelProps {
  alerts: PreparationQueueAlertResponse[];
  busy: boolean;
  onAcknowledge: (alertId: string) => void;
}

export function AlertsPanel({ alerts, busy, onAcknowledge }: AlertsPanelProps) {
  if (alerts.length === 0) return null;

  return (
    <div className="flex flex-col gap-2 p-3 rounded-2xl border border-amber-300 bg-amber-50">
      {alerts.map((alert) => (
        <div key={alert.id} className="flex items-center gap-3 p-2.5 rounded-xl bg-white border border-amber-200">
          <AlertTriangle className="w-4 h-4 text-amber-600 shrink-0" />
          <div className="flex-1 min-w-0 text-xs">
            <span className="font-bold">{(alert.kind && ALERT_KIND_LABELS[alert.kind]) || alert.kind}</span>
            {" — "}
            <span>
              {alert.item_name} #{alert.unit_number} (Đơn #{alert.service_number})
            </span>
            {alert.reason && <span className="text-muted-foreground"> · {alert.reason}</span>}
          </div>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={() => alert.id && onAcknowledge(alert.id)}
          >
            <Check className="w-3.5 h-3.5 mr-1" />
            Đã biết
          </Button>
        </div>
      ))}
    </div>
  );
}
```

Create `web/src/features/kds/components/corrections-log.tsx`:

```tsx
import { RotateCcw } from "lucide-react";
import { Accordion, AccordionItem, AccordionTrigger, AccordionContent } from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import type { PreparationQueueCorrectionResponse } from "@/api/generated/models";

export interface CorrectionsLogProps {
  corrections: PreparationQueueCorrectionResponse[];
  busy: boolean;
  onRemake: (entry: PreparationQueueCorrectionResponse) => void;
}

export function CorrectionsLog({ corrections, busy, onRemake }: CorrectionsLogProps) {
  if (corrections.length === 0) return null;

  return (
    <Accordion className="text-xs">
      <AccordionItem value="corrections">
        <AccordionTrigger>Hoạt động gần đây ({corrections.length})</AccordionTrigger>
        <AccordionContent>
          <div className="flex flex-col gap-2">
            {corrections.map((entry) => (
              <div key={entry.id} className="flex items-center gap-2 py-1">
                <span className="font-mono text-2xs text-muted-foreground w-14 shrink-0">
                  {entry.entry_kind === "WASTE" ? "Huỷ" : "Pha lại"}
                </span>
                <span className="flex-1 min-w-0 truncate">
                  {entry.item_name} #{entry.unit_number} (Đơn #{entry.service_number})
                </span>
                {entry.entry_kind === "WASTE" && entry.id && (
                  <Button type="button" size="xs" variant="outline" disabled={busy} onClick={() => onRemake(entry)}>
                    <RotateCcw className="w-3 h-3 mr-1" />
                    Pha lại
                  </Button>
                )}
              </div>
            ))}
          </div>
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  );
}
```

Create `web/src/features/kds/components/category-filter-bar.tsx`:

```tsx
import { cn } from "@/lib/utils";

export interface CategoryFilterBarProps {
  categories: string[];
  active: string | null;
  onSelect: (category: string | null) => void;
}

export function CategoryFilterBar({ categories, active, onSelect }: CategoryFilterBarProps) {
  if (categories.length === 0) return null;

  const chipClass = (isActive: boolean) =>
    cn(
      "shrink-0 h-9 px-3.5 rounded-full text-xs font-semibold border transition",
      isActive
        ? "bg-primary text-primary-foreground border-primary"
        : "bg-background border-border text-muted-foreground hover:bg-muted",
    );

  return (
    <div className="flex items-center gap-2 overflow-x-auto pb-1">
      <button type="button" onClick={() => onSelect(null)} className={chipClass(active === null)}>
        Tất cả
      </button>
      {categories.map((category) => (
        <button key={category} type="button" onClick={() => onSelect(category)} className={chipClass(active === category)}>
          {category}
        </button>
      ))}
    </div>
  );
}
```

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/features/kds/components/alerts-panel.test.tsx src/features/kds/components/corrections-log.test.tsx src/features/kds/components/category-filter-bar.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/kds/components/alerts-panel.tsx web/src/features/kds/components/alerts-panel.test.tsx web/src/features/kds/components/corrections-log.tsx web/src/features/kds/components/corrections-log.test.tsx web/src/features/kds/components/category-filter-bar.tsx web/src/features/kds/components/category-filter-bar.test.tsx
git commit -m "feat(web): add alerts panel, corrections log, and category filter"
```

---

## Task 9: Wire the KDS screen together

Replaces the `PlaceholderPage`-based `kds-view.tsx` with the real screen. The route file (`web/src/routes/_app/kds.tsx`) already imports `KdsView` from this exact path and already guards on `preparation.operate` — it needs no change.

**Files:**
- Create: `web/src/features/kds/components/kds-error-toast.tsx`
- Modify: `web/src/features/kds/components/kds-view.tsx` (currently the `PlaceholderPage` stub)

**Interfaces:**
- Consumes: `usePreparationQueue` (Task 2); `usePreparationActions` (Task 3); `buildBoard`, `distinctCategories`, `summarizeBulkOutcomes`, `COLUMN_KEYS`, `BoardUnit`, `ColumnKey` (Task 1); `QueueColumn` (Task 5); `AlertsPanel`, `CorrectionsLog`, `CategoryFilterBar` (Task 8); `WasteDialog` (Task 6); `CorrectStateDialog` (Task 7); `messageForError` from `@/lib/error-messages`.
- Produces: `export function KdsView(): React.ReactElement` (unchanged export name and zero-prop signature, so the existing route keeps working).

- [ ] **Step 1: Add the error toast**

Create `web/src/features/kds/components/kds-error-toast.tsx`:

```tsx
import { AlertCircle } from "lucide-react";

export function KdsErrorToast({ message, onDismiss }: { message: string | null; onDismiss: () => void }) {
  if (!message) return null;
  return (
    <div
      role="alert"
      className="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 flex items-center gap-2 rounded-xl bg-destructive text-destructive-foreground px-4 py-2.5 text-xs font-bold shadow-lg animate-in fade-in slide-in-from-bottom-2"
    >
      <AlertCircle className="h-4 w-4 shrink-0" />
      <span>{message}</span>
      <button
        type="button"
        onClick={onDismiss}
        className="ml-2 text-destructive-foreground/80 hover:text-destructive-foreground underline min-h-[48px] px-2 flex items-center"
      >
        Đóng
      </button>
    </div>
  );
}
```

- [ ] **Step 2: Replace the placeholder view**

Replace the full contents of `web/src/features/kds/components/kds-view.tsx` with:

```tsx
import { useEffect, useState } from "react";
import { ChefHat } from "lucide-react";
import { usePreparationQueue } from "../api/use-preparation-queue";
import { usePreparationActions } from "../api/use-preparation-actions";
import { buildBoard, distinctCategories, summarizeBulkOutcomes, COLUMN_KEYS, type BoardUnit, type ColumnKey } from "../lib/board";
import { QueueColumn } from "./queue-column";
import { AlertsPanel } from "./alerts-panel";
import { CorrectionsLog } from "./corrections-log";
import { CategoryFilterBar } from "./category-filter-bar";
import { WasteDialog } from "./waste-dialog";
import { CorrectStateDialog } from "./correct-state-dialog";
import { KdsErrorToast } from "./kds-error-toast";
import { messageForError } from "@/lib/error-messages";

/** Reasons Remake accepts (internal/preparation/domain.go's remakeReasons) — narrower than Waste's. */
const REMAKE_REASONS = new Set(["PREPARATION_ERROR", "QUALITY_FAILURE", "OTHER"]);

export function KdsView() {
  const { data, isLoading, isError } = usePreparationQueue();
  const actions = usePreparationActions();

  const [categoryFilter, setCategoryFilter] = useState<string | null>(null);
  const [wasteTarget, setWasteTarget] = useState<BoardUnit | null>(null);
  const [correctTarget, setCorrectTarget] = useState<{ unit: BoardUnit; column: ColumnKey } | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [nowMs, setNowMs] = useState(() => Date.now());

  // Elapsed-time badges only need to refresh coarsely; the queue's own 5s
  // poll already redraws the board on every real change.
  useEffect(() => {
    const timer = setInterval(() => setNowMs(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, []);

  const handleAdvance = async (unitIds: string[], targetState: string) => {
    try {
      if (unitIds.length === 1) {
        await actions.advanceUnit(unitIds[0], targetState);
        return;
      }
      const outcomes = await actions.advanceMany(unitIds, targetState);
      const { failed } = summarizeBulkOutcomes(outcomes);
      if (failed > 0) {
        setErrorMessage(`${failed} món không thể chuyển trạng thái do đã thay đổi. Danh sách đã được cập nhật.`);
      }
    } catch (err) {
      setErrorMessage(messageForError(err));
    }
  };

  const handleAcknowledge = async (alertId: string) => {
    try {
      await actions.acknowledgeAlert(alertId);
    } catch (err) {
      setErrorMessage(messageForError(err));
    }
  };

  // Carries the original Waste's own reason and note forward, rather than
  // asking the barista to re-describe an event that already happened.
  const handleRemake = async (entry: { id?: string; reason?: string; note?: string }) => {
    if (!entry.id) return;
    const reason = entry.reason && REMAKE_REASONS.has(entry.reason) ? entry.reason : "OTHER";
    try {
      await actions.remakeWaste(entry.id, reason, entry.note);
    } catch (err) {
      setErrorMessage(messageForError(err));
    }
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full text-sm text-muted-foreground p-8">
        Đang tải hàng chờ pha chế...
      </div>
    );
  }

  if (isError) {
    return (
      <div className="flex items-center justify-center h-full text-sm text-destructive p-8">
        Không tải được hàng chờ pha chế. Vui lòng tải lại trang.
      </div>
    );
  }

  const units = data?.units ?? [];
  const categories = distinctCategories(units);
  const board = buildBoard(units, categoryFilter);

  return (
    <div className="flex flex-col gap-4 p-4 h-full min-h-0">
      <div className="flex items-center gap-2">
        <ChefHat className="w-5 h-5 text-primary" />
        <h1 className="text-base font-bold text-foreground">Màn hình bếp</h1>
      </div>

      <AlertsPanel alerts={data?.alerts ?? []} busy={actions.isPending} onAcknowledge={handleAcknowledge} />

      <CategoryFilterBar categories={categories} active={categoryFilter} onSelect={setCategoryFilter} />

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4 flex-1 min-h-0">
        {COLUMN_KEYS.map((column) => (
          <QueueColumn
            key={column}
            column={column}
            tickets={board[column]}
            nowMs={nowMs}
            busy={actions.isPending}
            onAdvance={handleAdvance}
            onRequestWaste={setWasteTarget}
            onRequestCorrectState={(unit) => setCorrectTarget({ unit, column })}
          />
        ))}
      </div>

      <CorrectionsLog corrections={data?.corrections ?? []} busy={actions.isPending} onRemake={handleRemake} />

      <WasteDialog unit={wasteTarget} onClose={() => setWasteTarget(null)} />
      <CorrectStateDialog
        unit={correctTarget?.unit ?? null}
        column={correctTarget?.column ?? null}
        onClose={() => setCorrectTarget(null)}
      />

      <KdsErrorToast message={errorMessage} onDismiss={() => setErrorMessage(null)} />
    </div>
  );
}
```

- [ ] **Step 3: Run the full web test suite**

Run: `cd web && bun test`
Expected: PASS, no regressions in any other feature's tests.

- [ ] **Step 4: Run lint, typecheck, and build**

Run: `cd web && bun run lint && bunx tsc -b && bun run build`
Expected: Exit code 0 for all three. Lint warnings, if any, must be the same pre-existing `react(only-export-components)` warnings already present outside `src/features` — none should originate in `src/features/kds`.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/kds/components/kds-error-toast.tsx web/src/features/kds/components/kds-view.tsx
git commit -m "feat(web): wire the KDS screen to the real preparation queue API"
```

---

## Task 10: UAT gate

Per the sequence spec's definition of done (§3.7), the slice ends here — a handover script, not a self-certified "done". No push, no PR, no merge until the operator confirms.

**Files:**
- Create: `.superpowers/sdd/2026-09-26-web-slice-6-kds-preparation-queue/uat-handover.md` (only if using the SDD ledger workflow — otherwise hand the script directly to the user)

- [ ] **Step 1: Write the UAT handover script**

```markdown
# Web Slice 6 UAT handover

## Preconditions
- Go API running; `bun run scripts/dev-seed.ts` has seeded the catalog.
- A Sales Shift is open, and at least one Order has been submitted from the
  POS screen (Slice 5's "Gửi bếp") so the queue has active units.
- Checks run in the web app at `/kds`, signed in with a `preparation`
  workspace (or any role carrying `preparation.operate`).

## Happy path
1. Open `/kds`. Expect the submitted order's items grouped into one ticket
   card in the "Chờ pha" column.
2. Press "Bắt đầu làm" on the card. Expect it to move to "Đang pha" within
   5 seconds (or immediately on manual refresh).
3. Press "Xong món". Expect it to move to "Đã xong".
4. Press "Đã giao khách". Expect the ticket to disappear from the board
   entirely (Fulfilled units leave the active queue).
5. In a ticket with two or more items, check only one line's checkbox, then
   press the primary button. Expect only the checked unit to advance,
   leaving the rest of the ticket in its original column.
6. Pick a category filter chip. Expect only units of that category to show;
   "Tất cả" returns the full board.

## Waste and Remake
1. On a unit in "Đang pha", press the trash icon. Choose a reason, confirm.
   Expect the unit to leave the board and "Hoạt động gần đây" to show a
   "Huỷ" entry with a "Pha lại" button.
2. Press "Pha lại" on that entry. Expect a new unit to appear in "Chờ pha"
   with a red "PHA LẠI" badge, and the Alerts panel to show the original
   Waste alert until acknowledged.
3. Press "Đã biết" on the alert. Expect it to disappear from the panel.

## Correct-state (Manager Approval)
1. Advance a unit from "Chờ pha" to "Đang pha" by mistake. Press the undo
   icon next to it. Expect the Manager Approval dialog.
2. Enter a Manager's login code and PIN, confirm. Expect the unit to return
   to "Chờ pha".
3. Repeat with a wrong PIN. Expect a Vietnamese "not authorized" message and
   no state change.

## Unhappy paths
1. Disconnect the network, press a primary action. Expect a Vietnamese error
   toast, and the board unchanged once the network returns and the next
   poll lands.
2. Open `/kds` in two tabs. Advance a unit in one tab. Expect the other tab
   to reflect it within 5 seconds without a manual reload.

## Verification output

### `bun test`
(paste output)

### `bun run lint`
(paste output)

### `bunx tsc -b`
(paste output)

### `bun run build`
(paste output)
```

- [ ] **Step 2: Hand off**

Report to the user: all tasks complete, verification commands run and passing, this UAT script ready. Stop here — no push, no PR, no merge until the user runs UAT and confirms.

---
