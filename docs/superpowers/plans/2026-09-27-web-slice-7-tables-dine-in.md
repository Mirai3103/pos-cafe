# Web Slice 7 — Tables and Dine-in Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `/tables` placeholder with a live floor view and teach the POS the dine-in lifecycle: order in rounds, send each round to the bar before payment, collect at the end, change Tables, close.

**Architecture:** Takeaway keeps its linear `derivePosPhase` path untouched. Dine-in gets a parallel set of pure facts (`deriveDineInStatus`) and its own flow hook (`useDineInFlow`); `PosView` dispatches on `session.service_mode`. The floor view is a new feature module under `src/features/tables/` that reads `GET /tables/overview` by polling and hands a Session to the POS through the existing session pointer.

**Tech Stack:** React 19, TanStack Router + Query, orval-generated client (`src/api/generated`), Tailwind, lucide-react, `bun test` with `react-dom/server` `renderToString` for component tests.

**Spec:** [`docs/superpowers/specs/2026-09-27-web-slice-7-tables-dine-in-design.md`](../specs/2026-09-27-web-slice-7-tables-dine-in-design.md)

## Global Constraints

- All user-facing copy is Vietnamese, matching the strings in the spec verbatim ("Trống", "Có khách", "Tạm ngưng", "Gửi bếp", "Thu tiền", "Hoàn tất", "Về sơ đồ bàn", "Đổi bàn", "Mở phiên mới tại bàn này", "Gửi bếp lượt trước để gọi thêm", "Gửi bếp hoặc xóa món đang soạn trước khi thu tiền", "Chưa có bàn nào", "Chưa có bàn").
- Every mutation carries a `request_id` generated once per user intent (`newRequestId()` from `@/lib/command`), kept across retries of the same intent, dropped on success or when the intent is abandoned.
- Touch targets are at least 48px (`min-h-[48px]`), per `design-system/pos-cafe/DESIGN.md`.
- No client-held state machine: every decision is derived from the server's `SalesServiceSessionResponse`.
- Tests: pure logic and `renderToString` component checks only. No end-to-end, no integration tests (slice sequence §3).
- The takeaway path (`derivePosPhase`, `useCheckoutFlow`, `CheckPanel`, `CheckPanelActions`) must behave exactly as before.
- Overview poll interval: `10_000` ms.
- Run everything from `web/`: `bun test`, `bunx tsc -b`, `bun run lint`.

---

## File map

| File | Responsibility |
| --- | --- |
| `web/src/features/pos/utils/dine-in.ts` (new) | `deriveDineInStatus`, `dineInPhase`, `planSendToBar`, `canCollect`, `resolveDineInF9`, `needsNewRound`, `shouldPollSession`, `sessionTableLabel` |
| `web/src/features/tables/lib/floor.ts` (new) | `toFloorTables`, `tableCardState`, `floorStats`, `toggleSelection` |
| `web/src/features/tables/api/use-tables.ts` (new) | overview query, dine-in open, set tables, table admin commands |
| `web/src/lib/error-messages.ts` | new codes |
| `scripts/dev-seed.ts` | eight Tables |
| `web/src/features/tables/components/*` (new) | `table-card`, `open-table-dialog`, `session-picker`, `table-name-dialog`, rewritten `tables-view` |
| `web/src/routes/_app/tables.tsx` | capability guard |
| `web/src/features/pos/api/use-pos.ts` | `useStartNextDraft`, dine-in polling |
| `web/src/features/pos/api/use-pos-session.ts` | `selectPosSession`, `ensureDraft` |
| `web/src/features/pos/api/use-close-session.ts` | `isReady` option |
| `web/src/features/pos/api/use-dine-in.ts` (new) | `useDineInFlow` |
| `web/src/features/pos/components/dine-in-*.tsx`, `change-tables-dialog.tsx` (new) | dine-in panel, header, actions, change-tables dialog |
| `web/src/features/pos/components/pos-view.tsx` | mode dispatch |
| `web/src/features/pos/hooks/use-pos-hotkeys.ts` | dine-in F9 |
| `web/src/features/pos/utils/pending-orders.ts`, `pending-order-row.tsx` | dine-in rows |
| `web/src/features/pos/components/draft-panel.tsx` | "Tại bàn" goes to `/tables` |
| `ROADMAP.md` | slice 7 status |

`dine-in-panel.tsx` is not in the spec's file table; it is the composition that keeps `pos-view.tsx` growing "by dispatch only", as spec §6 requires.

---

### Task 1: Dine-in status logic

**Files:**
- Create: `web/src/features/pos/utils/dine-in.ts`
- Test: `web/src/features/pos/utils/dine-in.test.ts`

**Interfaces:**
- Consumes: `listLiveChecks`, `selectOpenCheck`, `hasMultipleOpenChecks`, `hasUnsubmittedWork`, `preparationProgress`, `PreparationProgress`, `PosPhase`, `derivePosPhase` from `./phase`.
- Produces:
  - `interface DineInStatus { draftItemCount: number; hasEditableDraft: boolean; hasUnsubmittedWork: boolean; openCheck: SalesCheckResponse | null; hasMultipleOpenChecks: boolean; progress: PreparationProgress; canClose: boolean; canOrder: boolean }`
  - `deriveDineInStatus(session: SalesServiceSessionResponse | null | undefined): DineInStatus`
  - `dineInPhase(status: DineInStatus): PosPhase` — reuses PosPhase values so pending-order labels and tones are shared
  - `planSendToBar(status: DineInStatus): { commit: boolean; submit: boolean }`
  - `canCollect(status: DineInStatus): boolean`
  - `type DineInF9Action = "none" | "send" | "collect" | "close"`; `resolveDineInF9(status: DineInStatus, blocked: boolean): DineInF9Action`
  - `isDineIn(session): boolean`
  - `needsNewRound(session): boolean`
  - `shouldPollSession(session): boolean`
  - `sessionTableLabel(session): string`

- [ ] **Step 1: Write the failing test**

`web/src/features/pos/utils/dine-in.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import type { SalesServiceSessionResponse } from "@/api/generated/models";
import {
  canCollect,
  deriveDineInStatus,
  dineInPhase,
  isDineIn,
  needsNewRound,
  planSendToBar,
  resolveDineInF9,
  sessionTableLabel,
  shouldPollSession,
} from "./dine-in";

type Session = SalesServiceSessionResponse;
type Check = NonNullable<Session["checks"]>[number];

function allocation(submitted: boolean) {
  return { id: "a1", name: "Bạc xỉu", allocated_quantity: 1, amount_vnd: 30_000, submitted };
}

function check(state: "OPEN" | "SETTLED", submitted: boolean, extra: Partial<Check> = {}): Check {
  return {
    id: `check-${state}`,
    state,
    charge_vnd: 30_000,
    balance_vnd: state === "OPEN" ? 30_000 : 0,
    created_at: "2026-09-27T01:00:00Z",
    allocations: [allocation(submitted)],
    payments: [],
    ...extra,
  };
}

function session(overrides: Partial<Session> = {}): Session {
  return {
    id: "s1",
    service_number: "012",
    service_mode: "DINE_IN",
    state: "ACTIVE",
    tables: [
      { id: "t1", name: "Bàn 1" },
      { id: "t2", name: "Bàn 2" },
    ],
    draft: null,
    checks: [],
    orders: [],
    preparation_units: [],
    ...overrides,
  } as Session;
}

const editableDraft = (count: number) => ({
  state: "EDITABLE",
  items: Array.from({ length: count }, (_, i) => ({ id: `d${i}`, name: "Trà đào", quantity: 1 })),
});

const order = { id: "o1" };
const unit = (state: string) => ({ id: `u-${state}`, state });

describe("deriveDineInStatus", () => {
  it("reads a fresh Session: empty editable draft, nothing to do", () => {
    const status = deriveDineInStatus(session({ draft: editableDraft(0) } as Partial<Session>));
    expect(status.hasEditableDraft).toBe(true);
    expect(status.draftItemCount).toBe(0);
    expect(status.openCheck).toBeNull();
    expect(status.canClose).toBe(false);
    expect(status.canOrder).toBe(true);
  });

  it("counts draft items only while the draft is editable", () => {
    expect(deriveDineInStatus(session({ draft: editableDraft(2) } as Partial<Session>)).draftItemCount).toBe(2);
    expect(
      deriveDineInStatus(session({ draft: { state: "COMMITTED", items: [{ id: "x" }] } } as Partial<Session>))
        .draftItemCount,
    ).toBe(0);
  });

  it("blocks ordering while a committed round has not been submitted", () => {
    const status = deriveDineInStatus(session({ checks: [check("OPEN", false)] }));
    expect(status.hasUnsubmittedWork).toBe(true);
    expect(status.canOrder).toBe(false);
  });

  it("exposes the open Check once a round is submitted but unpaid", () => {
    const status = deriveDineInStatus(
      session({ checks: [check("OPEN", true)], orders: [order], preparation_units: [unit("QUEUED")] } as Partial<Session>),
    );
    expect(status.openCheck?.id).toBe("check-OPEN");
    expect(status.canOrder).toBe(true);
    expect(status.canClose).toBe(false);
  });

  it("can close when settled, submitted, ordered, and every unit terminal", () => {
    const status = deriveDineInStatus(
      session({
        checks: [check("SETTLED", true)],
        orders: [order],
        preparation_units: [unit("FULFILLED"), unit("WASTED")],
      } as Partial<Session>),
    );
    expect(status.canClose).toBe(true);
  });

  it("does not let an empty editable draft block closure", () => {
    const status = deriveDineInStatus(
      session({
        draft: editableDraft(0),
        checks: [check("SETTLED", true)],
        orders: [order],
        preparation_units: [unit("FULFILLED")],
      } as Partial<Session>),
    );
    expect(status.canClose).toBe(true);
  });

  it("refuses closure while a unit is still in preparation", () => {
    const status = deriveDineInStatus(
      session({ checks: [check("SETTLED", true)], orders: [order], preparation_units: [unit("READY")] } as Partial<Session>),
    );
    expect(status.canClose).toBe(false);
  });

  it("refuses closure while a refund is pending", () => {
    const status = deriveDineInStatus(
      session({
        checks: [check("SETTLED", true, { pending_refund_vnd: 5_000 })],
        orders: [order],
        preparation_units: [unit("FULFILLED")],
      } as Partial<Session>),
    );
    expect(status.canClose).toBe(false);
  });

  it("flags more than one open Check", () => {
    const status = deriveDineInStatus(
      session({
        checks: [check("OPEN", true), { ...check("OPEN", true), id: "check-2" }],
        orders: [order],
      } as Partial<Session>),
    );
    expect(status.hasMultipleOpenChecks).toBe(true);
    expect(canCollect(status)).toBe(false);
  });
});

describe("dineInPhase", () => {
  it("prefers closure, then unsent work, then payment, then the bar, then drafting", () => {
    const ready = deriveDineInStatus(
      session({ checks: [check("SETTLED", true)], orders: [order], preparation_units: [unit("FULFILLED")] } as Partial<Session>),
    );
    expect(dineInPhase(ready)).toBe("READY_TO_CLOSE");

    const drafting = deriveDineInStatus(
      session({ draft: editableDraft(1), checks: [check("OPEN", true)], orders: [order] } as Partial<Session>),
    );
    expect(dineInPhase(drafting)).toBe("AWAITING_SUBMIT");

    const unpaid = deriveDineInStatus(
      session({ checks: [check("OPEN", true)], orders: [order], preparation_units: [unit("QUEUED")] } as Partial<Session>),
    );
    expect(dineInPhase(unpaid)).toBe("AWAITING_PAYMENT");

    const cooking = deriveDineInStatus(
      session({ checks: [check("SETTLED", true)], orders: [order], preparation_units: [unit("QUEUED")] } as Partial<Session>),
    );
    expect(dineInPhase(cooking)).toBe("IN_PREPARATION");

    expect(dineInPhase(deriveDineInStatus(session({ draft: editableDraft(0) } as Partial<Session>)))).toBe("DRAFTING");
  });
});

describe("planSendToBar", () => {
  it("commits and submits a drafted round", () => {
    expect(planSendToBar(deriveDineInStatus(session({ draft: editableDraft(2) } as Partial<Session>)))).toEqual({
      commit: true,
      submit: true,
    });
  });

  it("only submits a round whose commit already landed", () => {
    expect(planSendToBar(deriveDineInStatus(session({ checks: [check("OPEN", false)] })))).toEqual({
      commit: false,
      submit: true,
    });
  });

  it("does nothing when there is nothing to send", () => {
    expect(planSendToBar(deriveDineInStatus(session({ draft: editableDraft(0) } as Partial<Session>)))).toEqual({
      commit: false,
      submit: false,
    });
  });
});

describe("canCollect", () => {
  it("needs one open Check and an empty draft", () => {
    const base = { checks: [check("OPEN", true)], orders: [order] };
    expect(canCollect(deriveDineInStatus(session(base as Partial<Session>)))).toBe(true);
    expect(
      canCollect(deriveDineInStatus(session({ ...base, draft: editableDraft(1) } as Partial<Session>))),
    ).toBe(false);
    expect(canCollect(deriveDineInStatus(session({ draft: editableDraft(0) } as Partial<Session>)))).toBe(false);
  });
});

describe("resolveDineInF9", () => {
  it("fires send before collect before close, and nothing when blocked", () => {
    const sendable = deriveDineInStatus(session({ draft: editableDraft(1), checks: [check("OPEN", true)], orders: [order] } as Partial<Session>));
    expect(resolveDineInF9(sendable, false)).toBe("send");
    expect(resolveDineInF9(sendable, true)).toBe("none");

    const collectable = deriveDineInStatus(session({ checks: [check("OPEN", true)], orders: [order] } as Partial<Session>));
    expect(resolveDineInF9(collectable, false)).toBe("collect");

    const closable = deriveDineInStatus(
      session({ checks: [check("SETTLED", true)], orders: [order], preparation_units: [unit("FULFILLED")] } as Partial<Session>),
    );
    expect(resolveDineInF9(closable, false)).toBe("close");

    expect(resolveDineInF9(deriveDineInStatus(session({ draft: editableDraft(0) } as Partial<Session>)), false)).toBe("none");
  });
});

describe("needsNewRound", () => {
  it("opens a round only for a dine-in Session with no editable draft and nothing unsent", () => {
    expect(needsNewRound(session({ checks: [check("OPEN", true)], orders: [order] } as Partial<Session>))).toBe(true);
    expect(needsNewRound(session({ draft: editableDraft(0) } as Partial<Session>))).toBe(false);
    expect(needsNewRound(session({ checks: [check("OPEN", false)] }))).toBe(false);
    expect(needsNewRound(session({ service_mode: "TAKEAWAY" }))).toBe(false);
    expect(needsNewRound(null)).toBe(false);
  });
});

describe("shouldPollSession", () => {
  it("polls a dine-in Session with unfinished units even while a draft is open", () => {
    expect(
      shouldPollSession(session({ draft: editableDraft(1), orders: [order], preparation_units: [unit("QUEUED")] } as Partial<Session>)),
    ).toBe(true);
    expect(shouldPollSession(session({ draft: editableDraft(0) } as Partial<Session>))).toBe(false);
  });

  it("keeps the takeaway rule", () => {
    expect(
      shouldPollSession(
        session({ service_mode: "TAKEAWAY", checks: [check("SETTLED", true)], orders: [order], preparation_units: [unit("QUEUED")] } as Partial<Session>),
      ),
    ).toBe(true);
  });
});

describe("labels", () => {
  it("names Tables in order, or says there are none", () => {
    expect(sessionTableLabel(session())).toBe("Bàn 1, Bàn 2");
    expect(sessionTableLabel(session({ tables: [] }))).toBe("Chưa có bàn");
  });

  it("recognises dine-in", () => {
    expect(isDineIn(session())).toBe(true);
    expect(isDineIn(session({ service_mode: "TAKEAWAY" }))).toBe(false);
    expect(isDineIn(null)).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/pos/utils/dine-in.test.ts`
Expected: FAIL — `Cannot find module './dine-in'`.

- [ ] **Step 3: Write the implementation**

`web/src/features/pos/utils/dine-in.ts`:

```ts
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && bun test src/features/pos/utils/dine-in.test.ts`
Expected: PASS, all tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/pos/utils/dine-in.ts web/src/features/pos/utils/dine-in.test.ts
git commit -m "feat(web): derive dine-in status from the session projection"
```

---

### Task 2: Floor logic

**Files:**
- Create: `web/src/features/tables/lib/floor.ts`
- Test: `web/src/features/tables/lib/floor.test.ts`

**Interfaces:**
- Consumes: `TablesTableOverviewRow` from `@/api/generated/models`.
- Produces:
  - `type TableCardState = "FREE" | "OCCUPIED" | "UNAVAILABLE"`
  - `interface FloorOccupant { sessionId: string; serviceNumber: string }`
  - `interface FloorTable { id: string; name: string; available: boolean; occupants: FloorOccupant[]; state: TableCardState }`
  - `tableCardState(row: TablesTableOverviewRow): TableCardState`
  - `toFloorTables(rows: TablesTableOverviewRow[] | null | undefined): FloorTable[]`
  - `floorStats(tables: FloorTable[]): { total: number; free: number; occupied: number; unavailable: number }`
  - `toggleSelection(ids: string[], id: string): string[]`

- [ ] **Step 1: Write the failing test**

`web/src/features/tables/lib/floor.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { floorStats, tableCardState, toFloorTables, toggleSelection } from "./floor";

const occupant = { service_session_id: "s1", service_number: "012" };

describe("tableCardState", () => {
  it("is FREE when available and unoccupied", () => {
    expect(tableCardState({ id: "t", available: true, current_service_sessions: [] })).toBe("FREE");
  });

  it("is OCCUPIED whenever a Session sits there, available or not", () => {
    expect(tableCardState({ id: "t", available: true, current_service_sessions: [occupant] })).toBe("OCCUPIED");
    expect(tableCardState({ id: "t", available: false, current_service_sessions: [occupant] })).toBe("OCCUPIED");
  });

  it("is UNAVAILABLE when switched off and empty", () => {
    expect(tableCardState({ id: "t", available: false })).toBe("UNAVAILABLE");
  });
});

describe("toFloorTables", () => {
  it("maps rows, drops rows without an id, keeps server order", () => {
    const tables = toFloorTables([
      { id: "t2", name: "Bàn 2", available: true, current_service_sessions: [occupant] },
      { name: "no id" },
      { id: "t1", name: "Bàn 1", available: true },
    ]);
    expect(tables.map((t) => t.id)).toEqual(["t2", "t1"]);
    expect(tables[0]).toEqual({
      id: "t2",
      name: "Bàn 2",
      available: true,
      occupants: [{ sessionId: "s1", serviceNumber: "012" }],
      state: "OCCUPIED",
    });
  });

  it("answers an empty list for no data", () => {
    expect(toFloorTables(undefined)).toEqual([]);
  });
});

describe("floorStats", () => {
  it("counts each state", () => {
    const tables = toFloorTables([
      { id: "a", available: true },
      { id: "b", available: true, current_service_sessions: [occupant] },
      { id: "c", available: false },
    ]);
    expect(floorStats(tables)).toEqual({ total: 3, free: 1, occupied: 1, unavailable: 1 });
  });
});

describe("toggleSelection", () => {
  it("adds a missing id and removes a present one", () => {
    expect(toggleSelection(["a"], "b")).toEqual(["a", "b"]);
    expect(toggleSelection(["a", "b"], "a")).toEqual(["b"]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/tables/lib/floor.test.ts`
Expected: FAIL — `Cannot find module './floor'`.

- [ ] **Step 3: Write the implementation**

`web/src/features/tables/lib/floor.ts`:

```ts
import type { TablesTableOverviewRow } from "@/api/generated/models";

export type TableCardState = "FREE" | "OCCUPIED" | "UNAVAILABLE";

export interface FloorOccupant {
  sessionId: string;
  serviceNumber: string;
}

export interface FloorTable {
  id: string;
  name: string;
  available: boolean;
  occupants: FloorOccupant[];
  state: TableCardState;
}

/**
 * A Table still serving a party reads as occupied even after it is switched
 * off: the party is still there. Unavailability only stops new assignments.
 */
export function tableCardState(row: TablesTableOverviewRow): TableCardState {
  if ((row.current_service_sessions?.length ?? 0) > 0) return "OCCUPIED";
  return row.available ? "FREE" : "UNAVAILABLE";
}

export function toFloorTables(rows: TablesTableOverviewRow[] | null | undefined): FloorTable[] {
  return (rows ?? [])
    .filter((row) => Boolean(row.id))
    .map((row) => ({
      id: row.id!,
      name: row.name ?? "",
      available: row.available === true,
      occupants: (row.current_service_sessions ?? [])
        .filter((o) => Boolean(o.service_session_id))
        .map((o) => ({ sessionId: o.service_session_id!, serviceNumber: o.service_number ?? "" })),
      state: tableCardState(row),
    }));
}

export function floorStats(tables: FloorTable[]) {
  return {
    total: tables.length,
    free: tables.filter((t) => t.state === "FREE").length,
    occupied: tables.filter((t) => t.state === "OCCUPIED").length,
    unavailable: tables.filter((t) => t.state === "UNAVAILABLE").length,
  };
}

export function toggleSelection(ids: string[], id: string): string[] {
  return ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id];
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && bun test src/features/tables/lib/floor.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/tables/lib
git commit -m "feat(web): derive floor table state from the tables overview"
```

---

### Task 3: Tables API seam, session pointer, error messages

**Files:**
- Create: `web/src/features/tables/api/use-tables.ts`
- Test: `web/src/features/tables/api/use-tables.test.ts`
- Modify: `web/src/features/pos/api/use-pos-session.ts` (export `selectPosSession`)
- Modify: `web/src/lib/error-messages.ts` (add codes)
- Test: `web/src/lib/error-messages.test.ts` if it exists, otherwise create it

**Interfaces:**
- Consumes: generated `useGetTablesOverview`, `getGetTablesOverviewQueryKey`, `usePostTables`, `usePatchTablesTableIdName`, `usePatchTablesTableIdAvailability` (`@/api/generated/endpoints/tables/tables`); `usePostSalesServiceSessionsDineIn`, `usePutSalesServiceSessionsIdTables`, `getGetSalesServiceSessionsIdQueryKey`, `getGetSalesServiceSessionsQueryKey` (`@/api/generated/endpoints/sales/sales`).
- Produces:
  - `TABLES_OVERVIEW_POLL_MS = 10_000`
  - `useTablesOverview(): UseQueryResult<TablesTableOverviewRow[], ApiError>`
  - `useStartDineInSession(): { isPending: boolean; startDineIn(tableIds: string[], requestId: string): Promise<SalesServiceSessionResponse> }`
  - `useSetSessionTables(): { isPending: boolean; setTables(sessionId: string, tableIds: string[], requestId: string): Promise<SalesServiceSessionResponse> }`
  - `useTableAdmin(): { isPending: boolean; createTable(name: string, requestId: string): Promise<TablesTableResponse>; renameTable(tableId: string, name: string, requestId: string): Promise<TablesTableResponse>; setAvailability(tableId: string, available: boolean, requestId: string): Promise<TablesTableResponse> }`
  - `selectPosSession(id: string): void` in `use-pos-session.ts` — writes the pointer the POS reads on mount.

- [ ] **Step 1: Write the failing tests**

`web/src/features/tables/api/use-tables.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import {
  TABLES_OVERVIEW_POLL_MS,
  useSetSessionTables,
  useStartDineInSession,
  useTableAdmin,
  useTablesOverview,
} from "./use-tables";

describe("tables API seam", () => {
  it("polls the floor every ten seconds", () => {
    expect(TABLES_OVERVIEW_POLL_MS).toBe(10_000);
  });

  it("exports every hook the floor and the POS use", () => {
    for (const hook of [useTablesOverview, useStartDineInSession, useSetSessionTables, useTableAdmin]) {
      expect(typeof hook).toBe("function");
    }
  });
});
```

Check whether `web/src/lib/error-messages.test.ts` exists (`ls web/src/lib`). Add this test there, or create the file with the import line:

```ts
import { describe, expect, it } from "bun:test";
import { ERROR_MESSAGES } from "./error-messages";

describe("slice 7 error codes", () => {
  /** Pinned against internal/tables/errors.go and internal/sales/errors.go. */
  it("has a Vietnamese message for every Table and dine-in code", () => {
    for (const code of [
      "TABLE_NOT_FOUND",
      "TABLE_NAME_CONFLICT",
      "DINE_IN_TABLE_SELECTION_REQUIRED",
      "DINE_IN_TABLE_SELECTION_DUPLICATE",
      "DINE_IN_TABLE_NOT_FOUND",
      "DINE_IN_TABLE_UNAVAILABLE",
      "TAKEAWAY_TABLE_ASSIGNMENT_NOT_AVAILABLE",
      "NEW_ORDER_DRAFT_NOT_AVAILABLE",
    ]) {
      expect(ERROR_MESSAGES[code]).toBeTruthy();
    }
  });
});
```

Add to `web/src/features/pos/api/use-pos-session.test.ts`:

```ts
import { selectPosSession } from "./use-pos-session";

describe("selectPosSession", () => {
  it("stores the pointer the POS reads on mount", () => {
    selectPosSession("s-42");
    expect(sessionStorage.getItem("pos_active_session_id")).toBe("s-42");
  });
});
```

If `sessionStorage` is undefined under `bun test` (check with a one-line run), guard the test with `if (typeof sessionStorage === "undefined") return;` at the top of the `it` body.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun test src/features/tables/api src/lib src/features/pos/api/use-pos-session.test.ts`
Expected: FAIL — missing module `./use-tables`, missing `selectPosSession`, missing `TABLE_NOT_FOUND` message.

- [ ] **Step 3: Write the implementation**

`web/src/features/tables/api/use-tables.ts`:

```ts
import { useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import {
  useGetTablesOverview,
  getGetTablesOverviewQueryKey,
  usePostTables,
  usePatchTablesTableIdName,
  usePatchTablesTableIdAvailability,
} from "@/api/generated/endpoints/tables/tables";
import {
  usePostSalesServiceSessionsDineIn,
  usePutSalesServiceSessionsIdTables,
  getGetSalesServiceSessionsIdQueryKey,
  getGetSalesServiceSessionsQueryKey,
} from "@/api/generated/endpoints/sales/sales";
import type { TablesTableOverviewRow } from "@/api/generated/models";
import { unwrap, type ApiError } from "@/lib/unwrap";

/** Parties arrive and leave; there is no push channel, so the floor polls. */
export const TABLES_OVERVIEW_POLL_MS = 10_000;

export function useTablesOverview(): UseQueryResult<TablesTableOverviewRow[], ApiError> {
  return useGetTablesOverview<TablesTableOverviewRow[], ApiError>({
    query: { select: unwrap, refetchInterval: TABLES_OVERVIEW_POLL_MS },
  });
}

function useInvalidateFloor() {
  const queryClient = useQueryClient();
  return () => {
    void queryClient.invalidateQueries({ queryKey: getGetTablesOverviewQueryKey() });
    void queryClient.invalidateQueries({ queryKey: getGetSalesServiceSessionsQueryKey() });
  };
}

/** Seats a party: opens a dine-in Service Session at one or more Tables. */
export function useStartDineInSession() {
  const queryClient = useQueryClient();
  const invalidateFloor = useInvalidateFloor();
  const mutation = usePostSalesServiceSessionsDineIn();

  return {
    isPending: mutation.isPending,
    startDineIn: async (tableIds: string[], requestId: string) => {
      const res = await mutation.mutateAsync({ data: { request_id: requestId, table_ids: tableIds } });
      const data = unwrap(res);
      if (data.id) queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(data.id), res);
      invalidateFloor();
      return data;
    },
  };
}

/** Replaces a dine-in Session's Tables: move, add, and release in one command. */
export function useSetSessionTables() {
  const queryClient = useQueryClient();
  const invalidateFloor = useInvalidateFloor();
  const mutation = usePutSalesServiceSessionsIdTables();

  return {
    isPending: mutation.isPending,
    setTables: async (sessionId: string, tableIds: string[], requestId: string) => {
      const res = await mutation.mutateAsync({
        id: sessionId,
        data: { request_id: requestId, table_ids: tableIds },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sessionId), res);
      invalidateFloor();
      return data;
    },
  };
}

/** Table administration, for holders of tables.administer. */
export function useTableAdmin() {
  const invalidateFloor = useInvalidateFloor();
  const create = usePostTables();
  const rename = usePatchTablesTableIdName();
  const availability = usePatchTablesTableIdAvailability();

  return {
    isPending: create.isPending || rename.isPending || availability.isPending,
    createTable: async (name: string, requestId: string) => {
      const data = unwrap(await create.mutateAsync({ data: { request_id: requestId, name } }));
      invalidateFloor();
      return data;
    },
    renameTable: async (tableId: string, name: string, requestId: string) => {
      const data = unwrap(
        await rename.mutateAsync({ tableId, data: { request_id: requestId, name } }),
      );
      invalidateFloor();
      return data;
    },
    setAvailability: async (tableId: string, available: boolean, requestId: string) => {
      const data = unwrap(
        await availability.mutateAsync({ tableId, data: { request_id: requestId, available } }),
      );
      invalidateFloor();
      return data;
    },
  };
}
```

If `bunx tsc -b` reports that a generated mutation's variables differ (e.g. the path param is named `tableId` vs `table_id`), open `web/src/api/generated/endpoints/tables/tables.ts` and match its `MutationVariables` type exactly; the destructuring at lines 166–167 and 235–236 shows `{tableId, data}`.

In `web/src/features/pos/api/use-pos-session.ts`, add below `writeStoredSessionId`:

```ts
/**
 * Points this tab's POS at a Session before navigating to it. The floor view
 * uses it to hand a seated party to the cashier terminal.
 */
export function selectPosSession(id: string): void {
  writeStoredSessionId(id);
}
```

In `web/src/lib/error-messages.ts`, add to `ERROR_MESSAGES` (keep the existing `NEW_ORDER_DRAFT_NOT_AVAILABLE` entry, it already exists):

```ts
  TABLE_NOT_FOUND: "Không tìm thấy bàn.",
  TABLE_NAME_CONFLICT: "Tên bàn này đã tồn tại.",
  DINE_IN_TABLE_SELECTION_REQUIRED: "Vui lòng chọn ít nhất một bàn.",
  DINE_IN_TABLE_SELECTION_DUPLICATE: "Một bàn được chọn hai lần.",
  DINE_IN_TABLE_NOT_FOUND: "Một bàn đã chọn không còn tồn tại.",
  DINE_IN_TABLE_UNAVAILABLE: "Một bàn đã chọn đang tạm ngưng phục vụ.",
  TAKEAWAY_TABLE_ASSIGNMENT_NOT_AVAILABLE: "Đơn mang đi không gắn được bàn.",
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && bun test src/features/tables/api src/lib src/features/pos/api/use-pos-session.test.ts && bunx tsc -b`
Expected: PASS; tsc exits 0.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/tables/api web/src/features/pos/api/use-pos-session.ts web/src/features/pos/api/use-pos-session.test.ts web/src/lib
git commit -m "feat(web): tables API seam, session pointer handoff, table error messages"
```

---

### Task 4: Dev seed Tables

**Files:**
- Modify: `scripts/dev-seed.ts`

**Interfaces:**
- Consumes: `apiRequest`, the manager `token` already in `main()`.
- Produces: eight Tables named `Bàn 1` … `Bàn 8` on a fresh database; nothing on re-run.

- [ ] **Step 1: Add the seeding function**

In `scripts/dev-seed.ts`, add after the `MenuResponse` interface:

```ts
interface OverviewResponse {
  data?: Array<{ name?: string }>;
}

const SEED_TABLES = Array.from({ length: 8 }, (_, i) => `Bàn ${i + 1}`);

/** Creates each sample Table that does not exist yet, so re-running is harmless. */
async function seedTables(token: string): Promise<void> {
  const overview = await apiRequest<OverviewResponse>("/tables/overview", { method: "GET", token });
  const existing = new Set((overview?.data ?? []).map((row) => row.name));
  const missing = SEED_TABLES.filter((name) => !existing.has(name));
  for (const name of missing) {
    await apiRequest("/tables", { token, body: { name } });
  }
  console.log(missing.length > 0 ? `Created tables: ${missing.join(", ")}` : "Tables already seeded.");
}
```

- [ ] **Step 2: Call it before the catalog early return**

In `main()`, directly after the sign-in `try/catch` block (before `// 2. Check if Catalog is already seeded`), add:

```ts
  // Tables are seeded independently: the catalog check below returns early.
  try {
    await seedTables(token);
  } catch (error) {
    console.error("Failed to seed tables:", error);
  }
```

Also update the file's header comment line "Creates the first Manager and sample catalog entities" to "Creates the first Manager, sample Tables, and sample catalog entities".

- [ ] **Step 3: Verify**

With the Go server running (`make run` or the project's usual command) run `cd /e/Code/pos-cafe && bun run scripts/dev-seed.ts` twice.
Expected: first run prints `Created tables: Bàn 1, …, Bàn 8`; second prints `Tables already seeded.`
If no server is available, run `cd web && bunx tsc --noEmit ../scripts/dev-seed.ts --target es2022 --module esnext --moduleResolution bundler --types bun` or at minimum `bun build ../scripts/dev-seed.ts --target bun --outdir /tmp/seedcheck` to confirm it parses, and note in the handoff that the live run is a UAT step.

- [ ] **Step 4: Commit**

```bash
git add scripts/dev-seed.ts
git commit -m "chore(seed): create eight sample tables idempotently"
```

---

### Task 5: Floor view

**Files:**
- Create: `web/src/features/tables/components/table-card.tsx`
- Create: `web/src/features/tables/components/open-table-dialog.tsx`
- Create: `web/src/features/tables/components/session-picker.tsx`
- Create: `web/src/features/tables/components/table-name-dialog.tsx`
- Rewrite: `web/src/features/tables/components/tables-view.tsx`
- Modify: `web/src/routes/_app/tables.tsx`
- Test: `web/src/features/tables/components/table-card.test.tsx`, `open-table-dialog.test.tsx`, `tables-view.test.tsx`

**Interfaces:**
- Consumes: `FloorTable`, `toFloorTables`, `floorStats`, `toggleSelection` (Task 2); `useTablesOverview`, `useStartDineInSession`, `useTableAdmin` (Task 3); `selectPosSession` (Task 3); `useCurrentShift` from `@/features/shift/api/use-shift`; `useSessionStore` from `@/stores/use-session-store` (`capabilities: string[]`); `newRequestId`; `messageForError`; `requireCapability`.
- Produces:
  - `TableCard(props: { table: FloorTable; canAdminister: boolean; onOpen(table): void; onRename(table): void; onToggleAvailability(table): void })`
  - `OpenTableDialog(props: { initialTableId: string | null; tables: FloorTable[]; onClose(): void; onOpened(sessionId: string): void })` — renders `null` when `initialTableId` is null
  - `SessionPicker(props: { table: FloorTable | null; canOpenNew: boolean; onPick(sessionId: string): void; onOpenNew(table: FloorTable): void; onClose(): void })`
  - `TableNameDialog(props: { target: { mode: "create" } | { mode: "rename"; table: FloorTable } | null; onClose(): void })`
  - `TablesView()` default screen

The dialogs follow `web/src/features/kds/components/waste-dialog.tsx`: an outer component returning `null` without a target and an inner form keyed on the target, so each opening is one intent with one `request_id` held in a `useRef` for the form's lifetime; a fixed overlay `div` with `role="dialog"`, not a portal (so `renderToString` sees it).

- [ ] **Step 1: Write the failing tests**

`web/src/features/tables/components/table-card.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TableCard } from "./table-card";
import type { FloorTable } from "../lib/floor";

const noop = () => {};

function render(table: FloorTable, canAdminister = false) {
  return renderToString(
    <TableCard
      table={table}
      canAdminister={canAdminister}
      onOpen={noop}
      onRename={noop}
      onToggleAvailability={noop}
    />,
  );
}

const base: FloorTable = { id: "t1", name: "Bàn 1", available: true, occupants: [], state: "FREE" };

describe("TableCard", () => {
  it("shows a free Table", () => {
    const html = render(base);
    expect(html).toContain("Bàn 1");
    expect(html).toContain("Trống");
  });

  it("lists every Service Number on an occupied Table", () => {
    const html = render({
      ...base,
      state: "OCCUPIED",
      occupants: [
        { sessionId: "s1", serviceNumber: "012" },
        { sessionId: "s2", serviceNumber: "015" },
      ],
    });
    expect(html).toContain("Có khách");
    expect(html).toContain("#012");
    expect(html).toContain("#015");
  });

  it("marks an unavailable Table and disables it", () => {
    const html = render({ ...base, available: false, state: "UNAVAILABLE" });
    expect(html).toContain("Tạm ngưng");
    expect(html).toContain("disabled");
  });

  it("marks an occupied Table that has been switched off", () => {
    const html = render({
      ...base,
      available: false,
      state: "OCCUPIED",
      occupants: [{ sessionId: "s1", serviceNumber: "012" }],
    });
    expect(html).toContain("Có khách");
    expect(html).toContain("Tạm ngưng");
  });

  it("offers admin actions only to administrators", () => {
    expect(render(base)).not.toContain("Đổi tên");
    expect(render(base, true)).toContain("Đổi tên");
    expect(render(base, true)).toContain("Tạm ngưng");
    expect(render({ ...base, available: false, state: "UNAVAILABLE" }, true)).toContain("Mở lại");
  });
});
```

`web/src/features/tables/components/open-table-dialog.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToString } from "react-dom/server";
import { OpenTableDialog } from "./open-table-dialog";
import type { FloorTable } from "../lib/floor";

const tables: FloorTable[] = [
  { id: "t1", name: "Bàn 1", available: true, occupants: [], state: "FREE" },
  { id: "t2", name: "Bàn 2", available: true, occupants: [], state: "FREE" },
  { id: "t3", name: "Bàn 3", available: false, occupants: [], state: "UNAVAILABLE" },
];

function render(initialTableId: string | null) {
  return renderToString(
    <QueryClientProvider client={new QueryClient()}>
      <OpenTableDialog initialTableId={initialTableId} tables={tables} onClose={() => {}} onOpened={() => {}} />
    </QueryClientProvider>,
  );
}

describe("OpenTableDialog", () => {
  it("renders nothing without a Table", () => {
    expect(render(null)).toBe("");
  });

  it("offers every available Table and omits unavailable ones", () => {
    const html = render("t1");
    expect(html).toContain("Bàn 1");
    expect(html).toContain("Bàn 2");
    expect(html).not.toContain("Bàn 3");
    expect(html).toContain("Mở bàn");
  });

  it("preselects the tapped Table", () => {
    const html = render("t1");
    expect(html.split('aria-pressed="true"').length - 1).toBe(1);
  });
});
```

`web/src/features/tables/components/tables-view.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { FloorGrid } from "./tables-view";
import type { FloorTable } from "../lib/floor";

const tables: FloorTable[] = [
  { id: "t1", name: "Bàn 1", available: true, occupants: [], state: "FREE" },
];

describe("FloorGrid", () => {
  it("shows the empty state with the add action for administrators", () => {
    const html = renderToString(
      <FloorGrid tables={[]} canAdminister onOpen={() => {}} onRename={() => {}} onToggleAvailability={() => {}} onCreate={() => {}} />,
    );
    expect(html).toContain("Chưa có bàn nào");
    expect(html).toContain("Thêm bàn");
  });

  it("hides the add action from cashiers", () => {
    const html = renderToString(
      <FloorGrid tables={[]} canAdminister={false} onOpen={() => {}} onRename={() => {}} onToggleAvailability={() => {}} onCreate={() => {}} />,
    );
    expect(html).not.toContain("Thêm bàn");
  });

  it("renders a card per Table", () => {
    const html = renderToString(
      <FloorGrid tables={tables} canAdminister={false} onOpen={() => {}} onRename={() => {}} onToggleAvailability={() => {}} onCreate={() => {}} />,
    );
    expect(html).toContain("Bàn 1");
  });
});
```

`FloorGrid` is exported from `tables-view.tsx` as the presentational part so it can be tested without providers.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun test src/features/tables/components`
Expected: FAIL — modules not found.

- [ ] **Step 3: Implement `table-card.tsx`**

```tsx
import { MoreHorizontal, Pencil, Power } from "lucide-react";
import * as React from "react";
import type { FloorTable } from "../lib/floor";

export interface TableCardProps {
  table: FloorTable;
  canAdminister: boolean;
  onOpen: (table: FloorTable) => void;
  onRename: (table: FloorTable) => void;
  onToggleAvailability: (table: FloorTable) => void;
}

const STATE_CHIP: Record<FloorTable["state"], { label: string; tone: string }> = {
  FREE: { label: "Trống", tone: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300" },
  OCCUPIED: { label: "Có khách", tone: "bg-primary/10 text-primary" },
  UNAVAILABLE: { label: "Tạm ngưng", tone: "bg-muted text-muted-foreground" },
};

export function TableCard({ table, canAdminister, onOpen, onRename, onToggleAvailability }: TableCardProps) {
  const [isMenuOpen, setIsMenuOpen] = React.useState(false);
  const chip = STATE_CHIP[table.state];
  const switchedOff = !table.available && table.state === "OCCUPIED";

  return (
    <div className={`relative rounded-2xl border bg-card ${table.state === "UNAVAILABLE" ? "opacity-60" : ""}`}>
      <button
        type="button"
        onClick={() => onOpen(table)}
        disabled={table.state === "UNAVAILABLE"}
        className="w-full min-h-[120px] p-4 text-left flex flex-col justify-between gap-3 rounded-2xl hover:border-primary disabled:cursor-not-allowed select-none active:scale-[0.98] transition"
      >
        <div className="flex items-center justify-between gap-2 pr-10">
          <span className="text-base font-bold text-foreground">{table.name}</span>
          <span className={`rounded-md px-2 py-0.5 text-2xs font-bold ${chip.tone}`}>{chip.label}</span>
        </div>
        <div className="flex flex-wrap gap-1.5">
          {table.occupants.map((o) => (
            <span key={o.sessionId} className="rounded-md bg-muted px-2 py-0.5 font-mono text-xs font-semibold">
              {`#${o.serviceNumber}`}
            </span>
          ))}
          {switchedOff && (
            <span className="rounded-md bg-amber-100 text-amber-800 dark:bg-amber-950/60 dark:text-amber-300 px-2 py-0.5 text-2xs font-bold">
              Tạm ngưng
            </span>
          )}
        </div>
      </button>

      {canAdminister && (
        <div className="absolute top-2 right-2">
          <button
            type="button"
            aria-label={`Tùy chọn ${table.name}`}
            title={`Đổi tên / ${table.available ? "Tạm ngưng" : "Mở lại"}`}
            onClick={() => setIsMenuOpen((open) => !open)}
            className="h-12 w-12 min-h-[48px] rounded-xl flex items-center justify-center hover:bg-muted"
          >
            <MoreHorizontal className="h-4 w-4" />
          </button>
          {isMenuOpen && (
            <div role="menu" className="absolute right-0 z-10 mt-1 w-44 rounded-xl border border-border bg-card p-1 shadow-lg">
              <button
                type="button"
                role="menuitem"
                onClick={() => { setIsMenuOpen(false); onRename(table); }}
                className="w-full min-h-[48px] px-3 rounded-lg text-left text-sm flex items-center gap-2 hover:bg-muted"
              >
                <Pencil className="h-4 w-4" /> Đổi tên
              </button>
              <button
                type="button"
                role="menuitem"
                onClick={() => { setIsMenuOpen(false); onToggleAvailability(table); }}
                className="w-full min-h-[48px] px-3 rounded-lg text-left text-sm flex items-center gap-2 hover:bg-muted"
              >
                <Power className="h-4 w-4" /> {table.available ? "Tạm ngưng" : "Mở lại"}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
```

The menu starts closed, so the ⋯ button's `title` carries both action words; that is what the admin-visibility test sees in `renderToString`.

- [ ] **Step 4: Implement `open-table-dialog.tsx`**

```tsx
import { useRef, useState, type ReactElement } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { getGetTablesOverviewQueryKey } from "@/api/generated/endpoints/tables/tables";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { isConflictError } from "@/lib/unwrap";
import { useStartDineInSession } from "../api/use-tables";
import { toggleSelection, type FloorTable } from "../lib/floor";

export interface OpenTableDialogProps {
  initialTableId: string | null;
  tables: FloorTable[];
  onClose: () => void;
  onOpened: (sessionId: string) => void;
}

export function OpenTableDialog(props: OpenTableDialogProps): ReactElement | null {
  if (!props.initialTableId) return null;
  return <OpenTableForm key={props.initialTableId} {...props} initialTableId={props.initialTableId} />;
}

function OpenTableForm({
  initialTableId,
  tables,
  onClose,
  onOpened,
}: OpenTableDialogProps & { initialTableId: string }) {
  const queryClient = useQueryClient();
  const { startDineIn, isPending } = useStartDineInSession();
  const [selected, setSelected] = useState<string[]>([initialTableId]);
  const [error, setError] = useState<string | null>(null);
  // One seating is one intent: a retry after a lost response replays it.
  const requestIdRef = useRef(newRequestId());

  // Sharing a Table is legal, so the tapped one may already be occupied.
  const choices = tables.filter((t) => t.id === initialTableId || (t.available && t.state === "FREE"));

  async function handleConfirm() {
    if (isPending || selected.length === 0) return;
    setError(null);
    try {
      const session = await startDineIn(selected, requestIdRef.current);
      if (session.id) onOpened(session.id);
    } catch (err) {
      setError(messageForError(err));
      if (isConflictError(err)) {
        requestIdRef.current = newRequestId();
        void queryClient.invalidateQueries({ queryKey: getGetTablesOverviewQueryKey() });
      }
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 p-4 backdrop-blur-xs">
      <div role="dialog" aria-modal="true" aria-labelledby="open-table-title" className="flex w-full max-w-md flex-col gap-4 rounded-2xl border border-border bg-card p-6 shadow-xl">
        <div className="flex items-start justify-between">
          <div>
            <h2 id="open-table-title" className="text-base font-bold">Mở bàn</h2>
            <p className="text-xs text-muted-foreground">Chọn thêm bàn nếu khách ngồi ghép</p>
          </div>
          <button type="button" aria-label="Đóng" onClick={onClose} disabled={isPending} className="h-12 w-12 rounded-xl flex items-center justify-center hover:bg-muted">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="grid grid-cols-3 gap-2">
          {choices.map((t) => {
            const isOn = selected.includes(t.id);
            return (
              <button
                key={t.id}
                type="button"
                aria-pressed={isOn}
                onClick={() => setSelected((ids) => toggleSelection(ids, t.id))}
                className={`min-h-[48px] rounded-xl border px-3 text-sm font-semibold ${isOn ? "border-primary bg-primary/10 text-primary" : "border-border"}`}
              >
                {t.name}
              </button>
            );
          })}
        </div>
        {error && <p role="alert" className="text-xs font-semibold text-destructive">{error}</p>}
        <Button onClick={handleConfirm} disabled={isPending || selected.length === 0} className="h-12 min-h-[48px] rounded-xl font-bold">
          {isPending ? "Đang mở bàn..." : "Mở bàn"}
        </Button>
      </div>
    </div>
  );
}
```

A 409 means the floor changed under the operator (a Table was switched off): the floor is refetched and the re-made selection is a new intent with a new `request_id`.

- [ ] **Step 5: Implement `session-picker.tsx`**

```tsx
import { X } from "lucide-react";
import type { FloorTable } from "../lib/floor";

export interface SessionPickerProps {
  table: FloorTable | null;
  canOpenNew: boolean;
  onPick: (sessionId: string) => void;
  onOpenNew: (table: FloorTable) => void;
  onClose: () => void;
}

/** A Table serving more than one party: choose which one the cashier means. */
export function SessionPicker({ table, canOpenNew, onPick, onOpenNew, onClose }: SessionPickerProps) {
  if (!table) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 p-4 backdrop-blur-xs">
      <div role="dialog" aria-modal="true" aria-labelledby="session-picker-title" className="flex w-full max-w-sm flex-col gap-3 rounded-2xl border border-border bg-card p-6 shadow-xl">
        <div className="flex items-start justify-between">
          <h2 id="session-picker-title" className="text-base font-bold">{table.name}</h2>
          <button type="button" aria-label="Đóng" onClick={onClose} className="h-12 w-12 rounded-xl flex items-center justify-center hover:bg-muted">
            <X className="h-4 w-4" />
          </button>
        </div>
        {table.occupants.map((o) => (
          <button key={o.sessionId} type="button" onClick={() => onPick(o.sessionId)} className="min-h-[48px] rounded-xl border border-border px-4 text-left font-mono text-sm font-bold hover:bg-muted">
            {`#${o.serviceNumber}`}
          </button>
        ))}
        {canOpenNew && table.available && (
          <button type="button" onClick={() => onOpenNew(table)} className="min-h-[48px] rounded-xl bg-primary px-4 text-sm font-bold text-primary-foreground">
            Mở phiên mới tại bàn này
          </button>
        )}
      </div>
    </div>
  );
}
```

The spec calls this a popover; a small modal is used for touch reliability and to match the other dialogs. Record that in the PR description.

- [ ] **Step 6: Implement `table-name-dialog.tsx`**

```tsx
import { useRef, useState, type ReactElement } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { ApiError } from "@/lib/unwrap";
import { useTableAdmin } from "../api/use-tables";
import type { FloorTable } from "../lib/floor";

export type TableNameTarget = { mode: "create" } | { mode: "rename"; table: FloorTable };

export interface TableNameDialogProps {
  target: TableNameTarget | null;
  onClose: () => void;
}

export function TableNameDialog({ target, onClose }: TableNameDialogProps): ReactElement | null {
  if (!target) return null;
  const key = target.mode === "create" ? "create" : `rename-${target.table.id}`;
  return <TableNameForm key={key} target={target} onClose={onClose} />;
}

function TableNameForm({ target, onClose }: { target: TableNameTarget; onClose: () => void }) {
  const { createTable, renameTable, isPending } = useTableAdmin();
  const [name, setName] = useState(target.mode === "rename" ? target.table.name : "");
  const [error, setError] = useState<string | null>(null);
  const requestIdRef = useRef(newRequestId());

  async function handleSave() {
    const trimmed = name.trim();
    if (isPending || !trimmed) return;
    setError(null);
    try {
      if (target.mode === "create") await createTable(trimmed, requestIdRef.current);
      else await renameTable(target.table.id, trimmed, requestIdRef.current);
      onClose();
    } catch (err) {
      setError(messageForError(err));
      // The name was refused, so the next attempt carries a different name.
      if (err instanceof ApiError && err.code === "TABLE_NAME_CONFLICT") requestIdRef.current = newRequestId();
    }
  }

  const title = target.mode === "create" ? "Thêm bàn" : "Đổi tên bàn";
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 p-4 backdrop-blur-xs">
      <div role="dialog" aria-modal="true" aria-labelledby="table-name-title" className="flex w-full max-w-sm flex-col gap-4 rounded-2xl border border-border bg-card p-6 shadow-xl">
        <div className="flex items-start justify-between">
          <h2 id="table-name-title" className="text-base font-bold">{title}</h2>
          <button type="button" aria-label="Đóng" onClick={onClose} disabled={isPending} className="h-12 w-12 rounded-xl flex items-center justify-center hover:bg-muted">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="space-y-1">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Ví dụ: Bàn 9" className="h-12" autoFocus />
          {error && <p role="alert" className="text-xs font-semibold text-destructive">{error}</p>}
        </div>
        <Button onClick={handleSave} disabled={isPending || !name.trim()} className="h-12 min-h-[48px] rounded-xl font-bold">
          {isPending ? "Đang lưu..." : "Lưu"}
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 7: Rewrite `tables-view.tsx`**

```tsx
import * as React from "react";
import { useNavigate, Link } from "@tanstack/react-router";
import { AlertTriangle, Grid2X2, Plus, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useCurrentShift } from "@/features/shift/api/use-shift";
import { selectPosSession } from "@/features/pos/api/use-pos-session";
import { useSessionStore } from "@/stores/use-session-store";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { useTablesOverview, useTableAdmin } from "../api/use-tables";
import { floorStats, toFloorTables, type FloorTable } from "../lib/floor";
import { TableCard } from "./table-card";
import { OpenTableDialog } from "./open-table-dialog";
import { SessionPicker } from "./session-picker";
import { TableNameDialog, type TableNameTarget } from "./table-name-dialog";

export interface FloorGridProps {
  tables: FloorTable[];
  canAdminister: boolean;
  onOpen: (table: FloorTable) => void;
  onRename: (table: FloorTable) => void;
  onToggleAvailability: (table: FloorTable) => void;
  onCreate: () => void;
}

export function FloorGrid({ tables, canAdminister, onOpen, onRename, onToggleAvailability, onCreate }: FloorGridProps) {
  if (tables.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
        <Grid2X2 className="h-8 w-8 text-muted-foreground" />
        <p className="text-sm font-semibold">Chưa có bàn nào</p>
        {canAdminister && (
          <Button onClick={onCreate} className="h-12 min-h-[48px] rounded-xl font-bold">
            <Plus className="h-4 w-4 mr-2" /> Thêm bàn
          </Button>
        )}
      </div>
    );
  }
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-3">
      {tables.map((t) => (
        <TableCard key={t.id} table={t} canAdminister={canAdminister} onOpen={onOpen} onRename={onRename} onToggleAvailability={onToggleAvailability} />
      ))}
    </div>
  );
}

export function TablesView() {
  const navigate = useNavigate();
  const overview = useTablesOverview();
  const { data: shift } = useCurrentShift();
  const canAdminister = useSessionStore((s) => s.capabilities.includes("tables.administer"));
  const { setAvailability } = useTableAdmin();

  const [openTableId, setOpenTableId] = React.useState<string | null>(null);
  const [pickerTable, setPickerTable] = React.useState<FloorTable | null>(null);
  const [nameTarget, setNameTarget] = React.useState<TableNameTarget | null>(null);
  const [actionError, setActionError] = React.useState<string | null>(null);

  const isShiftOpen = shift?.state === "OPEN";
  const tables = toFloorTables(overview.data);
  const stats = floorStats(tables);

  const enterSession = (sessionId: string) => {
    selectPosSession(sessionId);
    void navigate({ to: "/" });
  };

  const handleOpen = (table: FloorTable) => {
    setActionError(null);
    if (table.occupants.length === 1) return enterSession(table.occupants[0].sessionId);
    if (table.occupants.length > 1) return setPickerTable(table);
    if (!isShiftOpen) return;
    setOpenTableId(table.id);
  };

  const handleToggle = async (table: FloorTable) => {
    setActionError(null);
    try {
      await setAvailability(table.id, !table.available, newRequestId());
    } catch (err) {
      setActionError(messageForError(err));
    }
  };

  return (
    <div className="flex h-full flex-col gap-4 overflow-y-auto p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Sơ đồ bàn</h1>
          <p className="text-xs text-muted-foreground">
            {`${stats.total} bàn · ${stats.free} trống · ${stats.occupied} có khách · ${stats.unavailable} tạm ngưng`}
          </p>
        </div>
        {canAdminister && tables.length > 0 && (
          <Button onClick={() => setNameTarget({ mode: "create" })} className="h-12 min-h-[48px] rounded-xl font-bold">
            <Plus className="h-4 w-4 mr-2" /> Thêm bàn
          </Button>
        )}
      </div>

      {!isShiftOpen && (
        <div role="status" className="flex flex-wrap items-center gap-3 rounded-xl border border-amber-200 bg-amber-50 dark:bg-amber-950/40 px-4 py-3 text-sm">
          <AlertTriangle className="h-4 w-4 text-amber-600" />
          <span className="flex-1">Chưa có ca bán hàng mở. Cần mở ca để nhận khách tại bàn.</span>
          <Button render={<Link to="/shift" />} variant="outline" className="h-12 min-h-[48px] rounded-xl">Mở ca làm việc</Button>
        </div>
      )}

      {actionError && <p role="alert" className="text-sm font-semibold text-destructive">{actionError}</p>}

      {overview.isLoading ? (
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-3">
          {Array.from({ length: 8 }, (_, i) => (
            <div key={i} className="min-h-[120px] rounded-2xl bg-muted animate-pulse" />
          ))}
        </div>
      ) : overview.isError ? (
        <div className="flex flex-col items-center gap-3 py-16 text-center">
          <p className="text-sm font-semibold text-destructive">{messageForError(overview.error)}</p>
          <Button variant="outline" onClick={() => void overview.refetch()} className="h-12 min-h-[48px] rounded-xl">
            <RefreshCw className="h-4 w-4 mr-2" /> Thử lại
          </Button>
        </div>
      ) : (
        <FloorGrid
          tables={tables}
          canAdminister={canAdminister}
          onOpen={handleOpen}
          onRename={(table) => setNameTarget({ mode: "rename", table })}
          onToggleAvailability={(table) => void handleToggle(table)}
          onCreate={() => setNameTarget({ mode: "create" })}
        />
      )}

      <OpenTableDialog initialTableId={openTableId} tables={tables} onClose={() => setOpenTableId(null)} onOpened={enterSession} />
      <SessionPicker
        table={pickerTable}
        canOpenNew={isShiftOpen}
        onPick={enterSession}
        onOpenNew={(table) => { setPickerTable(null); setOpenTableId(table.id); }}
        onClose={() => setPickerTable(null)}
      />
      <TableNameDialog target={nameTarget} onClose={() => setNameTarget(null)} />
    </div>
  );
}
```

The spec says the "reused `NoShiftNotice`"; that component is full-height and worded for the menu ("Bạn vẫn có thể xem thực đơn"), so the floor uses an inline banner with the same "Mở ca làm việc" link. Record in the PR description. A free Table tapped with no open Shift does nothing; the banner already explains why.

Availability toggles use a fresh `request_id` per tap: each tap is a distinct intent (on, then off, is two intents).

- [ ] **Step 8: Guard the route**

`web/src/routes/_app/tables.tsx`:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { TablesView } from "@/features/tables/components/tables-view";
import { requireCapability } from "@/lib/guards";

export const Route = createFileRoute("/_app/tables")({
  beforeLoad: () => requireCapability("sales.operate"),
  component: TablesView,
});
```

- [ ] **Step 9: Run tests, typecheck, lint**

Run: `cd web && bun test src/features/tables && bunx tsc -b && bun run lint`
Expected: PASS, tsc 0, lint clean (fix any `oxlint` findings in the new files).

- [ ] **Step 10: Commit**

```bash
git add web/src/features/tables web/src/routes/_app/tables.tsx
git commit -m "feat(web): live floor view with dine-in seating and table administration"
```

---

### Task 6: POS session plumbing for rounds

**Files:**
- Modify: `web/src/features/pos/api/use-pos.ts`
- Modify: `web/src/features/pos/api/use-pos-session.ts`
- Modify: `web/src/features/pos/api/use-close-session.ts`
- Test: `web/src/features/pos/api/use-pos.test.ts`, `use-pos-session.test.ts`, `use-close-session.test.ts`

**Interfaces:**
- Consumes: `needsNewRound`, `shouldPollSession` (Task 1); generated `usePostSalesServiceSessionsIdDraft`, `getGetSalesServiceSessionsIdQueryKey`.
- Produces:
  - `useStartNextDraft(): { startNextDraft(sessionId: string, requestId?: string): Promise<SalesServiceSessionResponse> }` in `use-pos.ts`
  - `PosSessionHandle.ensureDraft: (sessionId: string) => Promise<void>` in `use-pos-session.ts`
  - `recoverAfterRoundConflict(projection: SalesServiceSessionResponse | null | undefined): boolean` exported from `use-pos-session.ts` — true when the refetched Session already has an editable draft
  - `CloseFlowOptions.isReady?: boolean` in `use-close-session.ts`

- [ ] **Step 1: Write the failing tests**

Add to `web/src/features/pos/api/use-pos.test.ts`:

```ts
import { useStartNextDraft } from "./use-pos";

describe("useStartNextDraft", () => {
  it("is exported", () => {
    expect(typeof useStartNextDraft).toBe("function");
  });
});
```

Add to `web/src/features/pos/api/use-pos-session.test.ts`:

```ts
import { recoverAfterRoundConflict } from "./use-pos-session";

describe("recoverAfterRoundConflict", () => {
  it("proceeds when another terminal already opened the round", () => {
    expect(recoverAfterRoundConflict({ id: "s1", draft: { state: "EDITABLE", items: [] } })).toBe(true);
  });

  it("gives up when the round is still blocked", () => {
    expect(recoverAfterRoundConflict({ id: "s1", draft: null })).toBe(false);
    expect(recoverAfterRoundConflict(undefined)).toBe(false);
  });
});
```

Add to `web/src/features/pos/api/use-close-session.test.ts` (read the file first; it already imports from `./use-close-session`):

```ts
import { isCloseReady } from "./use-close-session";

describe("isCloseReady", () => {
  it("defers to an explicit readiness when the caller supplies one", () => {
    expect(isCloseReady({ id: "s1", draft: { state: "EDITABLE", items: [] } }, true)).toBe(true);
    expect(isCloseReady(null, false)).toBe(false);
  });

  it("falls back to the takeaway phase", () => {
    expect(isCloseReady(null, undefined)).toBe(false);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun test src/features/pos/api`
Expected: FAIL — `useStartNextDraft`, `recoverAfterRoundConflict`, `isCloseReady` not exported.

- [ ] **Step 3: Implement in `use-pos.ts`**

Add `usePostSalesServiceSessionsIdDraft` to the sales import list, import `shouldPollSession` from `../utils/dine-in`, and:

1. Replace `useServiceSession`'s `refetchInterval` with:

```ts
      refetchInterval: (query) =>
        shouldPollSession(query.state.data?.data) ? IN_PREPARATION_POLL_MS : false,
```

and remove the now-unused `derivePosPhase` import if nothing else in the file uses it.

2. Add:

```ts
/**
 * Opens a dine-in Session's next round. The server refuses while a draft is
 * editable or a committed round is unsent.
 */
export function useStartNextDraft() {
  const queryClient = useQueryClient();
  const mutation = usePostSalesServiceSessionsIdDraft();

  return {
    ...mutation,
    startNextDraft: async (sessionId: string, requestId?: string) => {
      const res = await mutation.mutateAsync({
        id: sessionId,
        data: { request_id: requestId ?? newRequestId() },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sessionId), res);
      return data;
    },
  };
}
```

- [ ] **Step 4: Implement in `use-pos-session.ts`**

Add imports:

```ts
import { useQueryClient } from "@tanstack/react-query";
import { getGetSalesServiceSessionsIdQueryKey } from "@/api/generated/endpoints/sales/sales";
import { ApiError } from "@/lib/unwrap";
import { needsNewRound } from "../utils/dine-in";
import { useServiceSession, useStartNextDraft, useStartTakeawaySession } from "./use-pos";
```

Add the exported helper:

```ts
/**
 * NEW_ORDER_DRAFT_NOT_AVAILABLE after a refetch: another terminal may already
 * have opened the round, in which case the add can go ahead.
 */
export function recoverAfterRoundConflict(
  projection: SalesServiceSessionResponse | null | undefined,
): boolean {
  return projection?.draft?.state === "EDITABLE";
}
```

Add `ensureDraft: (sessionId: string) => Promise<void>;` to `PosSessionHandle`. Inside `usePosSession`, after the existing refs:

```ts
  const queryClient = useQueryClient();
  const { startNextDraft } = useStartNextDraft();
  const sessionRef = React.useRef(session);
  React.useEffect(() => {
    sessionRef.current = session;
  }, [session]);
  const openingRoundRef = React.useRef<Promise<void> | null>(null);

  /**
   * A dine-in Session between rounds has no editable draft; the first item of
   * the next round opens one. Deduplicated like the lazy takeaway Session, so
   * a burst of taps opens one round, and never run otherwise, so sending a
   * round never leaves an empty draft behind.
   */
  const ensureDraft = React.useCallback(
    async (sessionId: string): Promise<void> => {
      const current = sessionRef.current;
      if (!current || current.id !== sessionId || !needsNewRound(current)) return;
      if (openingRoundRef.current) return await openingRoundRef.current;

      const promise = (async () => {
        try {
          await startNextDraft(sessionId);
        } catch (err) {
          if (!(err instanceof ApiError && err.code === "NEW_ORDER_DRAFT_NOT_AVAILABLE")) throw err;
          const key = getGetSalesServiceSessionsIdQueryKey(sessionId);
          await queryClient.refetchQueries({ queryKey: key });
          const cached = queryClient.getQueryData<{ data?: SalesServiceSessionResponse }>(key);
          if (!recoverAfterRoundConflict(cached?.data)) throw err;
        } finally {
          openingRoundRef.current = null;
        }
      })();

      openingRoundRef.current = promise;
      return await promise;
    },
    [startNextDraft, queryClient],
  );
```

Return `ensureDraft` alongside the other handle members.

- [ ] **Step 5: Implement in `use-close-session.ts`**

Add the exported helper and option:

```ts
/**
 * Whether closure may be attempted. Dine-in supplies its own readiness: an
 * open empty draft reads as DRAFTING to the takeaway phase, but does not
 * block closure on the server.
 */
export function isCloseReady(
  session: SalesServiceSessionResponse | null | undefined,
  isReady: boolean | undefined,
): boolean {
  return isReady ?? derivePosPhase(session) === "READY_TO_CLOSE";
}
```

Add to `CloseFlowOptions`:

```ts
  /** Overrides the takeaway readiness check; dine-in passes its canClose. */
  isReady?: boolean;
```

Destructure `isReady` in `useCloseFlow` and replace `if (derivePosPhase(session) !== "READY_TO_CLOSE") return;` with `if (!isCloseReady(session, isReady)) return;`.

- [ ] **Step 6: Run tests and typecheck**

Run: `cd web && bun test src/features/pos && bunx tsc -b`
Expected: PASS (including every pre-existing POS test), tsc 0.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/pos/api
git commit -m "feat(web): open dine-in rounds lazily and let dine-in supply close readiness"
```

---

### Task 7: `useDineInFlow`

**Files:**
- Create: `web/src/features/pos/api/use-dine-in.ts`
- Test: `web/src/features/pos/api/use-dine-in.test.ts`

**Interfaces:**
- Consumes: `useCommitDraft`, `usePayCash`, `useSubmitOrder`, `COMMIT_FAILURE_CODES`, `SUBMIT_ALREADY_DONE_CODES` (`./use-checkout`); `DineInStatus`, `planSendToBar`, `canCollect` (Task 1); `findCheckById` (`../utils/phase`); `latestPaymentChangeDue` (`../utils/payment`); `newRequestId`, `messageForError`, `ApiError`, `isConflictError`, `playSuccessChirp`, `playErrorBuzz`.
- Produces:
  - `interface DineInFlowOptions { activeSessionId: string | null; status: DineInStatus; onDraftError(message: string): void }`
  - `interface DineInFlow { isSending: boolean; sendError: string | null; sendToBar(): Promise<void>; isPaymentOpen: boolean; isPaying: boolean; paymentError: string | null; changeDueVnd: number | null; paymentTotalVnd: number; openPaymentDialog(): void; closePaymentDialog(): void; confirmPayment(tenderedVnd: number): Promise<void>; finishPayment(): void }`
  - `useDineInFlow(options: DineInFlowOptions): DineInFlow`
  - `classifySendFailure(err: unknown): "draft" | "done" | "retry"` — pure, exported for tests

- [ ] **Step 1: Write the failing test**

`web/src/features/pos/api/use-dine-in.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { ApiError } from "@/lib/unwrap";
import { classifySendFailure, useDineInFlow } from "./use-dine-in";

describe("classifySendFailure", () => {
  it("sends commit revalidation failures back to the draft", () => {
    expect(classifySendFailure(new ApiError(409, "COMMIT_SIZE_REQUIRED", "x"))).toBe("draft");
    expect(classifySendFailure(new ApiError(422, "EMPTY_DRAFT", "x"))).toBe("draft");
  });

  it("treats NOTHING_TO_SUBMIT as already sent", () => {
    expect(classifySendFailure(new ApiError(409, "NOTHING_TO_SUBMIT", "x"))).toBe("done");
  });

  it("leaves anything else on the retry button", () => {
    expect(classifySendFailure(new ApiError(500, "INTERNAL_ERROR", "x"))).toBe("retry");
    expect(classifySendFailure(new Error("network"))).toBe("retry");
  });
});

describe("useDineInFlow", () => {
  it("is exported", () => {
    expect(typeof useDineInFlow).toBe("function");
  });
});
```

Check the `ApiError` constructor order in `web/src/lib/unwrap.ts` (`new ApiError(status, code, message)` per its use in `unwrap`); adjust the test if it differs.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/pos/api/use-dine-in.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

`web/src/features/pos/api/use-dine-in.ts`:

```ts
import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { getGetSalesServiceSessionsIdQueryKey } from "@/api/generated/endpoints/sales/sales";
import { ApiError, isConflictError } from "@/lib/unwrap";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { playErrorBuzz, playSuccessChirp } from "@/lib/sound";
import {
  COMMIT_FAILURE_CODES,
  SUBMIT_ALREADY_DONE_CODES,
  useCommitDraft,
  usePayCash,
  useSubmitOrder,
} from "./use-checkout";
import { canCollect, planSendToBar, type DineInStatus } from "../utils/dine-in";
import { findCheckById } from "../utils/phase";
import { latestPaymentChangeDue } from "../utils/payment";

export function classifySendFailure(err: unknown): "draft" | "done" | "retry" {
  if (err instanceof ApiError && COMMIT_FAILURE_CODES.has(err.code)) return "draft";
  if (err instanceof ApiError && SUBMIT_ALREADY_DONE_CODES.has(err.code)) return "done";
  return "retry";
}

export interface DineInFlowOptions {
  activeSessionId: string | null;
  status: DineInStatus;
  /** Commit revalidation failures belong on the draft, via the page toast. */
  onDraftError: (message: string) => void;
}

export interface DineInFlow {
  isSending: boolean;
  sendError: string | null;
  sendToBar: () => Promise<void>;
  isPaymentOpen: boolean;
  isPaying: boolean;
  paymentError: string | null;
  changeDueVnd: number | null;
  paymentTotalVnd: number;
  openPaymentDialog: () => void;
  closePaymentDialog: () => void;
  confirmPayment: (tenderedVnd: number) => Promise<void>;
  finishPayment: () => void;
}

/**
 * Drives a seated party: each round goes to the bar before any money changes
 * hands, and the bill is collected at the end. Takeaway's useCheckoutFlow is
 * untouched; the two share only the commit, submit, and payment seams.
 */
export function useDineInFlow({ activeSessionId, status, onDraftError }: DineInFlowOptions): DineInFlow {
  const sid = activeSessionId ?? "";
  const queryClient = useQueryClient();
  const { commitDraft } = useCommitDraft(sid);
  const { submitOrder } = useSubmitOrder(sid);
  const { payCash } = usePayCash(sid);

  // One request id per intent, kept across retries so a replay after a lost
  // response reproduces the original outcome instead of duplicating it.
  const commitRequestIdRef = React.useRef<string | null>(null);
  const submitRequestIdRef = React.useRef<string | null>(null);
  const payRequestIdRef = React.useRef<string | null>(null);

  const [isSending, setIsSending] = React.useState(false);
  const [sendError, setSendError] = React.useState<string | null>(null);
  const [isPaymentOpen, setIsPaymentOpen] = React.useState(false);
  const [isPaying, setIsPaying] = React.useState(false);
  const [paymentError, setPaymentError] = React.useState<string | null>(null);
  const [changeDueVnd, setChangeDueVnd] = React.useState<number | null>(null);
  // Captured on open: once paid the Check settles and openCheck becomes null,
  // but the change screen must still show what was collected.
  const [dialogTotalVnd, setDialogTotalVnd] = React.useState(0);

  // Another Session carries other intents.
  React.useEffect(() => {
    commitRequestIdRef.current = null;
    submitRequestIdRef.current = null;
    payRequestIdRef.current = null;
    // oxlint-disable-next-line react/set-state-in-effect
    setSendError(null);
  }, [activeSessionId]);

  const refetchSession = () =>
    queryClient.invalidateQueries({ queryKey: getGetSalesServiceSessionsIdQueryKey(sid) });

  const sendToBar = async () => {
    if (!activeSessionId || isSending) return;
    const plan = planSendToBar(status);
    if (!plan.submit) return;

    setIsSending(true);
    setSendError(null);
    try {
      if (plan.commit) {
        commitRequestIdRef.current = commitRequestIdRef.current ?? newRequestId();
        await commitDraft(commitRequestIdRef.current, activeSessionId);
        commitRequestIdRef.current = null;
      }
      submitRequestIdRef.current = submitRequestIdRef.current ?? newRequestId();
      try {
        await submitOrder(submitRequestIdRef.current, activeSessionId);
      } catch (err) {
        if (classifySendFailure(err) !== "done") throw err;
        await refetchSession();
      }
      submitRequestIdRef.current = null;
      playSuccessChirp();
    } catch (err) {
      playErrorBuzz();
      const message = messageForError(err);
      if (classifySendFailure(err) === "draft") {
        // The draft is still editable; fixing it is a new intent.
        commitRequestIdRef.current = null;
        onDraftError(message);
      } else {
        // A landed commit shows as unsubmitted work, so the same button
        // retries the submit alone.
        setSendError(message);
        if (isConflictError(err)) void refetchSession();
      }
    } finally {
      setIsSending(false);
    }
  };

  const openPaymentDialog = () => {
    if (!canCollect(status)) return;
    payRequestIdRef.current = payRequestIdRef.current ?? newRequestId();
    setDialogTotalVnd(status.openCheck?.balance_vnd ?? 0);
    setPaymentError(null);
    setChangeDueVnd(null);
    setIsPaymentOpen(true);
  };

  const closePaymentDialog = () => {
    if (isPaying) return;
    setIsPaymentOpen(false);
    setPaymentError(null);
  };

  /** Pays the open Check in full. Never commits: rounds are sent separately. */
  const confirmPayment = async (tenderedVnd: number) => {
    const check = status.openCheck;
    if (!activeSessionId || !check?.id) return;
    setPaymentError(null);
    setIsPaying(true);
    try {
      const paid = await payCash(
        check.id,
        { applied_amount_vnd: check.balance_vnd ?? 0, cash_tendered_vnd: tenderedVnd },
        payRequestIdRef.current ?? undefined,
        activeSessionId,
      );
      setChangeDueVnd(latestPaymentChangeDue(findCheckById(paid, check.id)));
      playSuccessChirp();
    } catch (err) {
      playErrorBuzz();
      setPaymentError(messageForError(err));
      if (isConflictError(err)) void refetchSession();
    } finally {
      setIsPaying(false);
    }
  };

  const finishPayment = () => {
    setIsPaymentOpen(false);
    setChangeDueVnd(null);
    setPaymentError(null);
    payRequestIdRef.current = null;
  };

  return {
    isSending,
    sendError,
    sendToBar,
    isPaymentOpen,
    isPaying,
    paymentError,
    changeDueVnd,
    paymentTotalVnd: dialogTotalVnd,
    openPaymentDialog,
    closePaymentDialog,
    confirmPayment,
    finishPayment,
  };
}
```

- [ ] **Step 4: Run tests and typecheck**

Run: `cd web && bun test src/features/pos/api && bunx tsc -b`
Expected: PASS, tsc 0.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/pos/api/use-dine-in.ts web/src/features/pos/api/use-dine-in.test.ts
git commit -m "feat(web): dine-in flow sends rounds to the bar and collects at the end"
```

---

### Task 8: Dine-in panel components

**Files:**
- Create: `web/src/features/pos/components/dine-in-actions.tsx`
- Create: `web/src/features/pos/components/dine-in-header.tsx`
- Create: `web/src/features/pos/components/change-tables-dialog.tsx`
- Create: `web/src/features/pos/components/dine-in-panel.tsx`
- Test: `dine-in-actions.test.tsx`, `change-tables-dialog.test.tsx`, `dine-in-panel.test.tsx` in the same folder

**Interfaces:**
- Consumes: `DineInStatus`, `canCollect`, `planSendToBar`, `sessionTableLabel` (Task 1); `useTablesOverview`, `useSetSessionTables` (Task 3); `toFloorTables`, `toggleSelection`, `FloorTable` (Task 2); `DraftItemRow` (`./draft-item-row`); `listLiveChecks` (`../utils/phase`); `calculateDraftSubtotal` (`../utils/pricing`); `formatVND` (`@/lib/utils`).
- Produces:
  - `DineInActions(props: { status: DineInStatus; isShiftOpen: boolean; isSending: boolean; isClosing: boolean; onSend(): void; onCollect(): void; onClose(): void; onLeave(): void })`
  - `DineInHeader(props: { session: SalesServiceSessionResponse; onChangeTables(): void })`
  - `ChangeTablesDialog(props: { session: SalesServiceSessionResponse | null; onClose(): void })` — `null` session renders nothing; form uses `ChangeTablesForm` with `tables: FloorTable[]` read from `useTablesOverview`
  - `ChangeTablesChoices(props: { tables: FloorTable[]; sessionId: string; currentIds: string[]; selected: string[]; onToggle(id: string): void })` — presentational, exported for tests
  - `DineInPanel(props: { session: SalesServiceSessionResponse; status: DineInStatus; isShiftOpen: boolean; sendError: string | null; isSending: boolean; isClosing: boolean; onEditItem(item: SalesDraftItemResponse): void; onQuantityChange(itemId: string, qty: number): void; onRemoveItem(itemId: string): void; onSend(): void; onCollect(): void; onClose(): void; onLeave(): void; onChangeTables(): void; className?: string })`

- [ ] **Step 1: Write the failing tests**

`web/src/features/pos/components/dine-in-actions.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { DineInActions } from "./dine-in-actions";
import type { DineInStatus } from "../utils/dine-in";

const idle: DineInStatus = {
  draftItemCount: 0,
  hasEditableDraft: true,
  hasUnsubmittedWork: false,
  openCheck: null,
  hasMultipleOpenChecks: false,
  progress: { done: 0, total: 0 },
  canClose: false,
  canOrder: true,
};

const noop = () => {};
function render(status: DineInStatus) {
  return renderToString(
    <DineInActions status={status} isShiftOpen isSending={false} isClosing={false} onSend={noop} onCollect={noop} onClose={noop} onLeave={noop} />,
  );
}

describe("DineInActions", () => {
  it("always offers the way back to the floor", () => {
    const html = render(idle);
    expect(html).toContain("Về sơ đồ bàn");
    expect(html).not.toContain("Gửi bếp");
    expect(html).not.toContain("Thu tiền");
    expect(html).not.toContain("Hoàn tất");
  });

  it("offers Gửi bếp for a drafted round", () => {
    expect(render({ ...idle, draftItemCount: 2 })).toContain("Gửi bếp (F9)");
  });

  it("disables Thu tiền while the draft has items, and says why", () => {
    const html = render({ ...idle, draftItemCount: 1, openCheck: { id: "c1", balance_vnd: 30_000 } });
    expect(html).toContain("Thu tiền");
    expect(html).toContain("Gửi bếp hoặc xóa món đang soạn trước khi thu tiền");
  });

  it("offers Thu tiền as the primary action once everything is sent", () => {
    expect(render({ ...idle, openCheck: { id: "c1", balance_vnd: 30_000 } })).toContain("Thu tiền (F9)");
  });

  it("offers Hoàn tất when the Session can close", () => {
    expect(render({ ...idle, canClose: true })).toContain("Hoàn tất (F9)");
  });
});
```

`web/src/features/pos/components/change-tables-dialog.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { ChangeTablesChoices, ChangeTablesDialog } from "./change-tables-dialog";
import type { FloorTable } from "@/features/tables/lib/floor";

const tables: FloorTable[] = [
  { id: "t1", name: "Bàn 1", available: true, occupants: [{ sessionId: "s1", serviceNumber: "012" }], state: "OCCUPIED" },
  { id: "t2", name: "Bàn 2", available: true, occupants: [], state: "FREE" },
  { id: "t3", name: "Bàn 3", available: true, occupants: [{ sessionId: "s9", serviceNumber: "020" }], state: "OCCUPIED" },
  { id: "t4", name: "Bàn 4", available: false, occupants: [], state: "UNAVAILABLE" },
];

describe("ChangeTablesDialog", () => {
  it("renders nothing without a Session", () => {
    expect(renderToString(<ChangeTablesDialog session={null} onClose={() => {}} />)).toBe("");
  });
});

describe("ChangeTablesChoices", () => {
  it("lists held and available Tables, marks other parties, omits unavailable ones", () => {
    const html = renderToString(
      <ChangeTablesChoices tables={tables} sessionId="s1" currentIds={["t1"]} selected={["t1"]} onToggle={() => {}} />,
    );
    expect(html).toContain("Bàn 1");
    expect(html).not.toContain("#012");
    expect(html).toContain("Bàn 2");
    expect(html).toContain("Bàn 3");
    expect(html).toContain("#020");
    expect(html).not.toContain("Bàn 4");
  });
});
```

`web/src/features/pos/components/dine-in-panel.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { DineInPanel } from "./dine-in-panel";
import { deriveDineInStatus } from "../utils/dine-in";
import type { SalesServiceSessionResponse } from "@/api/generated/models";

const session = {
  id: "s1",
  service_number: "012",
  service_mode: "DINE_IN",
  state: "ACTIVE",
  tables: [{ id: "t1", name: "Bàn 1" }],
  draft: { state: "EDITABLE", items: [] },
  checks: [
    {
      id: "c1",
      state: "OPEN",
      charge_vnd: 30_000,
      balance_vnd: 30_000,
      allocations: [{ id: "a1", name: "Bạc xỉu", allocated_quantity: 1, amount_vnd: 30_000, submitted: true }],
      payments: [],
    },
  ],
  orders: [{ id: "o1" }],
  preparation_units: [{ id: "u1", state: "QUEUED" }],
} as SalesServiceSessionResponse;

const noop = () => {};

describe("DineInPanel", () => {
  it("shows the Tables, the sent items, and the balance owed", () => {
    const html = renderToString(
      <DineInPanel
        session={session}
        status={deriveDineInStatus(session)}
        isShiftOpen
        sendError={null}
        isSending={false}
        isClosing={false}
        onEditItem={noop}
        onQuantityChange={noop}
        onRemoveItem={noop}
        onSend={noop}
        onCollect={noop}
        onClose={noop}
        onLeave={noop}
        onChangeTables={noop}
      />,
    );
    expect(html).toContain("Bàn 1");
    expect(html).toContain("#012");
    expect(html).toContain("Bạc xỉu");
    expect(html).toContain("Đã gửi bếp");
    expect(html).toContain("0/1");
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun test src/features/pos/components/dine-in-actions.test.tsx src/features/pos/components/change-tables-dialog.test.tsx src/features/pos/components/dine-in-panel.test.tsx`
Expected: FAIL — modules not found.

- [ ] **Step 3: Implement `dine-in-actions.tsx`**

```tsx
import type { ReactNode } from "react";
import { ArrowLeft, Banknote, CheckCircle2, ChefHat } from "lucide-react";
import { canCollect, planSendToBar, resolveDineInF9, type DineInStatus } from "../utils/dine-in";

export interface DineInActionsProps {
  status: DineInStatus;
  isShiftOpen: boolean;
  isSending: boolean;
  isClosing: boolean;
  onSend: () => void;
  onCollect: () => void;
  onClose: () => void;
  onLeave: () => void;
}

const PRIMARY =
  "min-h-[48px] h-12 w-full rounded-xl bg-primary text-sm font-bold text-primary-foreground disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2 select-none active:scale-[0.98] transition";
const SECONDARY =
  "min-h-[48px] h-12 w-full rounded-xl border border-border bg-card text-sm font-bold text-foreground hover:bg-muted disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2 select-none active:scale-[0.98] transition";

/**
 * Every action whose condition holds, the F9 one primary. The key label must
 * match resolveDineInF9, which use-pos-hotkeys.ts fires.
 */
export function DineInActions({ status, isShiftOpen, isSending, isClosing, onSend, onCollect, onClose, onLeave }: DineInActionsProps) {
  const f9 = resolveDineInF9(status, false);
  const showSend = planSendToBar(status).submit;
  const showCollect = status.openCheck !== null && !status.hasMultipleOpenChecks;
  const collectBlockedByDraft = showCollect && !canCollect(status);
  const key = (action: typeof f9, label: string): ReactNode => (f9 === action ? `${label} (F9)` : label);

  return (
    <div className="border-t border-border bg-muted/20 p-4 space-y-2.5 shrink-0">
      {showSend && (
        <button type="button" onClick={onSend} disabled={isSending || !isShiftOpen} className={f9 === "send" ? PRIMARY : SECONDARY}>
          <ChefHat className="h-4 w-4" />
          {isSending ? "Đang gửi bếp..." : key("send", "Gửi bếp")}
        </button>
      )}
      {showCollect && (
        <>
          <button type="button" onClick={onCollect} disabled={!canCollect(status)} className={f9 === "collect" ? PRIMARY : SECONDARY}>
            <Banknote className="h-4 w-4" />
            {key("collect", "Thu tiền")}
          </button>
          {collectBlockedByDraft && (
            <p className="text-2xs text-muted-foreground text-center">
              Gửi bếp hoặc xóa món đang soạn trước khi thu tiền
            </p>
          )}
        </>
      )}
      {status.canClose && (
        <button type="button" onClick={onClose} disabled={isClosing} className={f9 === "close" ? PRIMARY : SECONDARY}>
          <CheckCircle2 className="h-4 w-4" />
          {isClosing ? "Đang hoàn tất..." : key("close", "Hoàn tất")}
        </button>
      )}
      <button type="button" onClick={onLeave} className={SECONDARY}>
        <ArrowLeft className="h-4 w-4" />
        Về sơ đồ bàn
      </button>
    </div>
  );
}
```

- [ ] **Step 4: Implement `dine-in-header.tsx`**

```tsx
import { ArrowLeftRight, Utensils } from "lucide-react";
import type { SalesServiceSessionResponse } from "@/api/generated/models";
import { sessionTableLabel } from "../utils/dine-in";

export interface DineInHeaderProps {
  session: SalesServiceSessionResponse;
  onChangeTables: () => void;
}

export function DineInHeader({ session, onChangeTables }: DineInHeaderProps) {
  return (
    <div className="flex items-center justify-between gap-2 border-b border-border p-4 bg-muted/20 shrink-0">
      <div className="flex items-center gap-2.5 min-w-0">
        <div className="h-9 w-9 rounded-xl bg-primary/10 text-primary flex items-center justify-center shrink-0">
          <Utensils className="h-5 w-5" />
        </div>
        <div className="min-w-0">
          <p className="text-sm font-bold text-foreground truncate">
            {`${sessionTableLabel(session)} · #${session.service_number ?? ""}`}
          </p>
          <p className="text-2xs text-muted-foreground">Khách dùng tại bàn</p>
        </div>
      </div>
      <button type="button" onClick={onChangeTables} className="h-12 min-h-[48px] px-3 rounded-xl border border-border text-xs font-semibold flex items-center gap-1.5 hover:bg-muted shrink-0">
        <ArrowLeftRight className="h-4 w-4" /> Đổi bàn
      </button>
    </div>
  );
}
```

- [ ] **Step 5: Implement `change-tables-dialog.tsx`**

```tsx
import { useRef, useState, type ReactElement } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { getGetTablesOverviewQueryKey } from "@/api/generated/endpoints/tables/tables";
import type { SalesServiceSessionResponse } from "@/api/generated/models";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { isConflictError } from "@/lib/unwrap";
import { useSetSessionTables, useTablesOverview } from "@/features/tables/api/use-tables";
import { toFloorTables, toggleSelection, type FloorTable } from "@/features/tables/lib/floor";

export interface ChangeTablesChoicesProps {
  tables: FloorTable[];
  /** This Session; its own occupancy is not "another party". */
  sessionId: string;
  currentIds: string[];
  selected: string[];
  onToggle: (id: string) => void;
}

/** Held Tables plus every available one; sharing a Table with another party is legal. */
export function ChangeTablesChoices({ tables, sessionId, currentIds, selected, onToggle }: ChangeTablesChoicesProps) {
  const choices = tables.filter((t) => currentIds.includes(t.id) || t.available);
  return (
    <div className="grid grid-cols-2 gap-2">
      {choices.map((t) => {
        const isOn = selected.includes(t.id);
        const others = t.occupants.filter((o) => o.sessionId !== sessionId);
        return (
          <button
            key={t.id}
            type="button"
            aria-pressed={isOn}
            onClick={() => onToggle(t.id)}
            className={`min-h-[48px] rounded-xl border px-3 py-2 text-left ${isOn ? "border-primary bg-primary/10" : "border-border"}`}
          >
            <span className="block text-sm font-semibold">{t.name}</span>
            {others.length > 0 && (
              <span className="block font-mono text-2xs text-muted-foreground">
                {others.map((o) => `#${o.serviceNumber}`).join(" ")}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

export interface ChangeTablesDialogProps {
  session: SalesServiceSessionResponse | null;
  onClose: () => void;
}

export function ChangeTablesDialog({ session, onClose }: ChangeTablesDialogProps): ReactElement | null {
  if (!session?.id) return null;
  return <ChangeTablesForm key={session.id} session={session} onClose={onClose} />;
}

function ChangeTablesForm({ session, onClose }: { session: SalesServiceSessionResponse; onClose: () => void }) {
  const queryClient = useQueryClient();
  const overview = useTablesOverview();
  const { setTables, isPending } = useSetSessionTables();
  const currentIds = (session.tables ?? []).map((t) => t.id ?? "").filter(Boolean);
  const [selected, setSelected] = useState<string[]>(currentIds);
  const [error, setError] = useState<string | null>(null);
  const requestIdRef = useRef(newRequestId());

  async function handleSave() {
    if (isPending || selected.length === 0 || !session.id) return;
    setError(null);
    try {
      await setTables(session.id, selected, requestIdRef.current);
      onClose();
    } catch (err) {
      setError(messageForError(err));
      if (isConflictError(err)) {
        requestIdRef.current = newRequestId();
        void queryClient.invalidateQueries({ queryKey: getGetTablesOverviewQueryKey() });
      }
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 p-4 backdrop-blur-xs">
      <div role="dialog" aria-modal="true" aria-labelledby="change-tables-title" className="flex w-full max-w-md flex-col gap-4 rounded-2xl border border-border bg-card p-6 shadow-xl">
        <div className="flex items-start justify-between">
          <div>
            <h2 id="change-tables-title" className="text-base font-bold">Đổi bàn</h2>
            <p className="text-xs text-muted-foreground">Chọn các bàn khách đang ngồi. Cần ít nhất một bàn.</p>
          </div>
          <button type="button" aria-label="Đóng" onClick={onClose} disabled={isPending} className="h-12 w-12 rounded-xl flex items-center justify-center hover:bg-muted">
            <X className="h-4 w-4" />
          </button>
        </div>
        <ChangeTablesChoices
          tables={toFloorTables(overview.data)}
          sessionId={session.id ?? ""}
          currentIds={currentIds}
          selected={selected}
          onToggle={(id) => setSelected((ids) => toggleSelection(ids, id))}
        />
        {error && <p role="alert" className="text-xs font-semibold text-destructive">{error}</p>}
        <Button onClick={handleSave} disabled={isPending || selected.length === 0} className="h-12 min-h-[48px] rounded-xl font-bold">
          {isPending ? "Đang lưu..." : "Lưu"}
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 6: Implement `dine-in-panel.tsx`**

```tsx
import { AlertCircle, AlertTriangle } from "lucide-react";
import type { SalesDraftItemResponse, SalesServiceSessionResponse } from "@/api/generated/models";
import { formatVND } from "@/lib/utils";
import { DraftItemRow } from "./draft-item-row";
import { DineInHeader } from "./dine-in-header";
import { DineInActions } from "./dine-in-actions";
import { listLiveChecks } from "../utils/phase";
import { calculateDraftSubtotal } from "../utils/pricing";
import type { DineInStatus } from "../utils/dine-in";

export interface DineInPanelProps {
  session: SalesServiceSessionResponse;
  status: DineInStatus;
  isShiftOpen: boolean;
  sendError: string | null;
  isSending: boolean;
  isClosing: boolean;
  onEditItem: (item: SalesDraftItemResponse) => void;
  onQuantityChange: (itemId: string, nextQty: number) => void;
  onRemoveItem: (itemId: string) => void;
  onSend: () => void;
  onCollect: () => void;
  onClose: () => void;
  onLeave: () => void;
  onChangeTables: () => void;
  className?: string;
}

/**
 * The seated party's bill: the round being drafted, then what the bar already
 * has, then what is owed. Committed amounts are the server's frozen snapshot;
 * only the drafted round is priced locally, for display.
 */
export function DineInPanel(props: DineInPanelProps) {
  const { session, status, className } = props;
  const draftItems = status.hasEditableDraft ? (session.draft?.items ?? []) : [];
  const checks = listLiveChecks(session);
  const allocations = checks.flatMap((c) => c.allocations ?? []);
  const owed = checks.reduce((sum, c) => sum + (c.balance_vnd ?? 0), 0);
  const { done, total } = status.progress;

  return (
    <aside className={`flex flex-col bg-card overflow-hidden select-none ${className ?? ""}`}>
      <DineInHeader session={session} onChangeTables={props.onChangeTables} />

      <div className="flex-1 overflow-y-auto p-4 space-y-4 bg-muted/10">
        <section className="space-y-2">
          <h3 className="text-2xs font-bold uppercase tracking-wider text-muted-foreground">Lượt đang gọi</h3>
          {!status.canOrder && (
            <p className="rounded-xl bg-amber-100 text-amber-900 dark:bg-amber-950/60 dark:text-amber-200 px-3 py-2 text-xs font-semibold">
              Gửi bếp lượt trước để gọi thêm
            </p>
          )}
          {draftItems.length > 0 ? (
            draftItems.map((item) => (
              <DraftItemRow key={item.id} item={item} onEdit={props.onEditItem} onQuantityChange={props.onQuantityChange} onRemove={props.onRemoveItem} disabled={!props.isShiftOpen} />
            ))
          ) : (
            status.canOrder && <p className="text-xs text-muted-foreground">Chọn món để bắt đầu lượt mới</p>
          )}
          {draftItems.length > 0 && (
            <p className="text-right font-mono text-xs text-muted-foreground">{`Tạm tính ${formatVND(calculateDraftSubtotal(draftItems))}`}</p>
          )}
        </section>

        {allocations.length > 0 && (
          <section className="space-y-2">
            <div className="flex items-center justify-between">
              <h3 className="text-2xs font-bold uppercase tracking-wider text-muted-foreground">Đã gửi bếp</h3>
              {total > 0 && <span className="font-mono text-2xs tabular-nums text-muted-foreground">{`${done}/${total} món xong`}</span>}
            </div>
            {allocations.map((a) => (
              <div key={a.id} className="flex items-start justify-between gap-2 rounded-xl border border-border bg-card p-3">
                <span className="text-sm font-semibold">{`${a.allocated_quantity ?? 0} × ${a.name ?? ""}`}</span>
                <span className="font-mono text-sm tabular-nums">{formatVND(a.amount_vnd ?? 0)}</span>
              </div>
            ))}
          </section>
        )}
      </div>

      {status.hasMultipleOpenChecks && (
        <div role="alert" className="mx-4 mb-2 flex items-start gap-2 rounded-xl bg-destructive/10 text-destructive px-3 py-2.5 text-xs font-semibold">
          <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5" />
          <span>Phiên này có nhiều hóa đơn đang mở. Chức năng tách hóa đơn chưa hỗ trợ ở phiên bản này, vui lòng báo quản lý.</span>
        </div>
      )}
      {props.sendError && (
        <div role="alert" className="mx-4 mb-2 flex items-start gap-2 rounded-xl bg-amber-100 text-amber-900 dark:bg-amber-950/60 dark:text-amber-200 px-3 py-2.5 text-xs font-semibold">
          <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
          <span>{`Chưa gửi được bếp. ${props.sendError}`}</span>
        </div>
      )}

      <div className="border-t border-border bg-card p-4 flex items-baseline justify-between shrink-0">
        <span className="text-sm font-bold">Còn phải thu</span>
        <span className="font-mono text-2xl font-bold text-primary tabular-nums">{formatVND(owed)}</span>
      </div>

      <DineInActions
        status={status}
        isShiftOpen={props.isShiftOpen}
        isSending={props.isSending}
        isClosing={props.isClosing}
        onSend={props.onSend}
        onCollect={props.onCollect}
        onClose={props.onClose}
        onLeave={props.onLeave}
      />
    </aside>
  );
}
```

Check `DraftItemRow`'s prop names in `web/src/features/pos/components/draft-item-row.tsx` before relying on them (`item`, `onEdit`, `onQuantityChange`, `onRemove`, `disabled` as used by `draft-panel.tsx`).

- [ ] **Step 7: Run tests, typecheck, lint**

Run: `cd web && bun test src/features/pos/components && bunx tsc -b && bun run lint`
Expected: PASS, tsc 0, lint clean.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/pos/components/dine-in-*.tsx web/src/features/pos/components/change-tables-dialog*.tsx
git commit -m "feat(web): dine-in bill panel, action bar, and change-tables dialog"
```

---

### Task 9: Wire dine-in into the POS

**Files:**
- Modify: `web/src/features/pos/components/pos-view.tsx`
- Modify: `web/src/features/pos/hooks/use-pos-hotkeys.ts`
- Modify: `web/src/features/pos/utils/pending-orders.ts`
- Modify: `web/src/features/pos/components/pending-order-row.tsx`
- Modify: `web/src/features/pos/components/draft-panel.tsx`
- Test: `use-pos-hotkeys.test.ts`, `pending-orders.test.ts`, `pos-view.test.tsx` (existing files)

**Interfaces:**
- Consumes: everything from Tasks 1, 3, 6, 7, 8.
- Produces:
  - `PendingOrder.tableLabel: string | null` — Table names for dine-in, `null` for takeaway
  - `PosHotkeysOptions.dineIn?: { status: DineInStatus; onSend(): Promise<void>; onCollect(): void; onClose(): Promise<void> }`

- [ ] **Step 1: Write the failing tests**

Add to `web/src/features/pos/utils/pending-orders.test.ts`:

```ts
describe("dine-in rows", () => {
  const dineIn = {
    id: "d1",
    service_number: "020",
    service_mode: "DINE_IN",
    state: "ACTIVE",
    created_at: "2026-09-27T01:00:00Z",
    tables: [{ id: "t1", name: "Bàn 5" }],
    draft: null,
    checks: [
      {
        id: "c1",
        state: "OPEN",
        charge_vnd: 30_000,
        balance_vnd: 30_000,
        allocations: [{ id: "a1", name: "Bạc xỉu", amount_vnd: 30_000, submitted: true }],
        payments: [],
      },
    ],
    orders: [{ id: "o1" }],
    preparation_units: [{ id: "u1", state: "FULFILLED" }],
  };

  it("lists dine-in Sessions with their Tables and dine-in label", () => {
    const [row] = toPendingOrders([dineIn] as never);
    expect(row.sessionId).toBe("d1");
    expect(row.tableLabel).toBe("Bàn 5");
    expect(row.phase).toBe("AWAITING_PAYMENT");
  });

  it("keeps takeaway rows without a Table label", () => {
    const [row] = toPendingOrders([{ ...dineIn, id: "t", service_mode: "TAKEAWAY", tables: [] }] as never);
    expect(row.tableLabel).toBeNull();
  });
});
```

Update any existing test in that file that asserted dine-in Sessions are filtered out: it now expects them included. Read the file and change that assertion.

Add to `web/src/features/pos/hooks/use-pos-hotkeys.test.ts` — no new hook test is needed (the pure `resolveDineInF9` is covered in Task 1); keep the existing `resolveF9Action` tests passing.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && bun test src/features/pos/utils/pending-orders.test.ts`
Expected: FAIL — dine-in row missing / `tableLabel` undefined.

- [ ] **Step 3: Update `pending-orders.ts`**

Import `deriveDineInStatus`, `dineInPhase`, `isDineIn`, `sessionTableLabel` from `./dine-in`. Add `tableLabel: string | null;` to `PendingOrder`. In `toPendingOrders`, change the filter to `.filter((session) => Boolean(session.id))` and the mapping to:

```ts
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
```

`isDrafting`, `itemNames`, and `totalVnd` read the editable draft first; for a dine-in Session that has both a draft and sent rounds, make them combine both. Replace them with:

```ts
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
```

For takeaway this is equivalent: a takeaway Session has either an editable draft or Checks, never both. Delete the old `isDrafting`.

- [ ] **Step 4: Update `pending-order-row.tsx`**

Replace the service-number span with:

```tsx
        <span className="text-sm font-bold text-foreground truncate">
          {order.tableLabel ? `${order.tableLabel} · #${order.serviceNumber}` : `#${order.serviceNumber}`}
        </span>
```

- [ ] **Step 5: Update `use-pos-hotkeys.ts`**

Import `resolveDineInF9` and `type DineInStatus` from `../utils/dine-in`. Add to `PosHotkeysOptions`:

```ts
  /** Present while a dine-in Session is active: F9 follows the dine-in action bar. */
  dineIn?: {
    status: DineInStatus;
    onSend: () => Promise<void>;
    onCollect: () => void;
    onClose: () => Promise<void>;
  };
```

Destructure `dineIn`, and at the top of the F9 callback, after `event.preventDefault();`:

```ts
      if (dineIn) {
        switch (resolveDineInF9(dineIn.status, blocked || isDrawerOpen)) {
          case "send":
            void dineIn.onSend();
            break;
          case "collect":
            dineIn.onCollect();
            break;
          case "close":
            void dineIn.onClose();
            break;
          default:
            break;
        }
        return;
      }
```

- [ ] **Step 6: Update `pos-view.tsx`**

Add imports:

```ts
import { useNavigate } from "@tanstack/react-router";
import { useDineInFlow } from "../api/use-dine-in";
import { deriveDineInStatus, isDineIn as isDineInSession } from "../utils/dine-in";
import { DineInPanel } from "./dine-in-panel";
import { ChangeTablesDialog } from "./change-tables-dialog";
```

Take `ensureDraft` from `usePosSession()`. After `const phase = derivePosPhase(session);` add:

```ts
  const navigate = useNavigate();
  const isDineIn = isDineInSession(session);
  const dineInStatus = deriveDineInStatus(session);
  const dineIn = useDineInFlow({ activeSessionId, status: dineInStatus, onDraftError: setErrorMessage });
  const [isChangingTables, setIsChangingTables] = React.useState(false);

  const leaveToFloor = () => {
    clearSession();
    void navigate({ to: "/tables" });
  };
```

Pass `isReady: isDineIn ? dineInStatus.canClose : undefined` into `useCloseFlow({...})`.

In both add paths (`handleSelectItem` simple add and `handleConfirmPicker` new-item add), after `const sid = await ensureSessionId();` insert `await ensureDraft(sid);`.

Replace the `MenuGrid` `disabled` prop with:

```tsx
            disabled={
              !isShiftOpen ||
              (isDineIn ? !dineInStatus.canOrder : phase !== "NO_SESSION" && phase !== "DRAFTING")
            }
```

Add `dineIn` to `usePosHotkeys` and include the change-tables dialog in `blocked`:

```ts
    blocked:
      checkout.isPaymentOpen || dineIn.isPaymentOpen || isChangingTables || isPickerOpen || closeFlow.completedSale !== null,
    dineIn: isDineIn
      ? { status: dineInStatus, onSend: dineIn.sendToBar, onCollect: dineIn.openPaymentDialog, onClose: closeFlow.closeSession }
      : undefined,
```

Replace the right-hand panel's conditional with a three-way choice:

```tsx
          {isDineIn && session ? (
            <DineInPanel
              session={session}
              status={dineInStatus}
              isShiftOpen={isShiftOpen}
              sendError={dineIn.sendError}
              isSending={dineIn.isSending}
              isClosing={closeFlow.isClosing}
              onEditItem={handleEditDraftItem}
              onQuantityChange={handleQuantityChange}
              onRemoveItem={handleRemoveItem}
              onSend={() => void dineIn.sendToBar()}
              onCollect={dineIn.openPaymentDialog}
              onClose={() => void closeFlow.closeSession()}
              onLeave={leaveToFloor}
              onChangeTables={() => setIsChangingTables(true)}
              className="w-full h-full flex-1"
            />
          ) : phase === "AWAITING_PAYMENT" || isPostPaymentPhase(phase) ? (
            /* existing CheckPanel unchanged */
          ) : (
            /* existing DraftPanel unchanged */
          )}
```

(Keep the existing `CheckPanel` and `DraftPanel` JSX exactly as they are in those two branches.)

Replace the single `PaymentDialog` with a mode-aware one:

```tsx
      <PaymentDialog
        isOpen={isDineIn ? dineIn.isPaymentOpen : checkout.isPaymentOpen}
        serviceNumber={session?.service_number}
        totalVnd={isDineIn ? dineIn.paymentTotalVnd : paymentTotal}
        isCommitted={isDineIn || phase === "AWAITING_PAYMENT"}
        isSubmitting={isDineIn ? dineIn.isPaying : checkout.isPaying}
        errorMessage={isDineIn ? dineIn.paymentError : checkout.paymentError}
        changeDueVnd={isDineIn ? dineIn.changeDueVnd : checkout.changeDueVnd}
        submitStatus={isDineIn ? "idle" : checkout.submitStatus}
        submitError={isDineIn ? null : checkout.submitError}
        onClose={isDineIn ? dineIn.closePaymentDialog : checkout.closePaymentDialog}
        onConfirm={isDineIn ? dineIn.confirmPayment : checkout.confirmPayment}
        onDone={isDineIn ? dineIn.finishPayment : checkout.finishPayment}
      />
```

Make the completed-sale dialog return dine-in to the floor:

```tsx
      <CompletedSaleDialog
        sale={closeFlow.completedSale}
        onDone={() => {
          const wasDineIn = isDineIn;
          closeFlow.dismissCompletedSale();
          if (wasDineIn) void navigate({ to: "/tables" });
        }}
      />
```

And mount the change-tables dialog:

```tsx
      <ChangeTablesDialog
        session={isChangingTables && isDineIn ? session : null}
        onClose={() => setIsChangingTables(false)}
      />
```

- [ ] **Step 7: Enable "Tại bàn" in `draft-panel.tsx`**

The disabled toggle's tooltip still promises Slice 7. Make it a link to the floor:

```tsx
import { Link } from "@tanstack/react-router";
```

and replace the disabled `<button … title="Chế độ Tại bàn sẽ hoạt động ở Slice 7">…</button>` with:

```tsx
          <Link
            to="/tables"
            className="min-h-[40px] h-10 px-3 rounded-lg font-medium text-xs text-foreground hover:bg-card flex items-center justify-center gap-1.5"
          >
            <Utensils className="h-3.5 w-3.5" />
            <span>Tại bàn</span>
          </Link>
```

The label drops "(F2)": no F2 binding exists. If `pos-view.test.tsx` or `draft-panel.test.tsx` renders `DraftPanel` with `renderToString` and no router, a bare `Link` may throw; if so, wrap the affected test render in the same router test harness other tests use, or render a plain `<a href="/tables">` instead of `Link`. Choose `<a href="/tables">` if no harness exists — a full navigation to `/tables` is acceptable here.

- [ ] **Step 8: Run the whole suite, typecheck, lint, build**

Run: `cd web && bun test && bunx tsc -b && bun run lint && bun run build`
Expected: every test PASS (including every pre-existing takeaway test), tsc 0, lint clean, build succeeds.

After `bun run build`, `web/dist/.gitkeep` may be deleted by Vite; restore it with `git checkout -- web/dist/.gitkeep` before committing.

- [ ] **Step 9: Commit**

```bash
git add web/src/features/pos
git commit -m "feat(web): dine-in sessions on the POS terminal and pending orders"
```

---

### Task 10: Roadmap and UAT handoff

**Files:**
- Modify: `ROADMAP.md`

- [ ] **Step 1: Update the roadmap**

In `ROADMAP.md`, in the frontend paragraph under **Delivered** that lists the web specs, append after the slice-sequence link:

```markdown
[slice 7, tables and dine-in](docs/superpowers/specs/2026-09-27-web-slice-7-tables-dine-in-design.md).
```

and change the sentence in **Remaining** "slices 1 through 6 of the sequence satisfy its Sign-in, Cashier, and Preparation Queue criteria." to "slices 1 through 6 of the sequence satisfy its Sign-in, Cashier, and Preparation Queue criteria; slice 7 adds Tables and dine-in."

- [ ] **Step 2: Final verification**

Run: `cd web && bun test && bunx tsc -b && bun run lint`
Expected: all green. Paste the summary lines into the handoff.

- [ ] **Step 3: Commit**

```bash
git add ROADMAP.md
git commit -m "docs: record web slice 7 in the roadmap"
```

- [ ] **Step 4: Stop at the UAT gate**

Do not self-certify. Hand the operator the spec's §8 script verbatim (12 steps) and wait for confirmation before opening the pull request. Record in the PR description the three recorded deviations from the spec: session picker is a small modal rather than a popover; the floor uses an inline no-shift banner rather than `NoShiftNotice`; `dine-in-panel.tsx` added as the composition component.
