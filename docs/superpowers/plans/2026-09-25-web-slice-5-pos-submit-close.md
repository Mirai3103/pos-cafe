# Web Slice 5: POS-c Submit, Session Closure & Completed Sale Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Send every paid takeaway order to the bar automatically. Show the cashier which orders are still waiting. Close an order once the kitchen has finished it, and show its immutable Completed Sale.

**Architecture:** `derivePosPhase` stays the only source of POS phase. Slice 4's `SETTLED` splits into `AWAITING_SUBMIT`, `IN_PREPARATION`, and `READY_TO_CLOSE`, each derived from `allocations[].submitted` and `preparation_units[].state`. The checkout flow appends submit to commit → pay, and every mutation writes the returned projection into the react-query cache. A polled "Đơn đang chờ" drawer lists every active takeaway session; tapping one makes it the active session, so the existing right-hand panel supplies the action for its phase. Closure is gated on the kitchen (spec section 2).

**Tech Stack:** React 19, TanStack Query v5, Tailwind CSS v4, Lucide icons, react-hotkeys-hook, Web Audio feedback (`playTapChirp` / `playSuccessChirp` / `playErrorBuzz`), `bun test`, orval-generated client in `web/src/api/generated`.

**Spec:** [`docs/superpowers/specs/2026-09-25-web-slice-5-pos-submit-close-design.md`](../specs/2026-09-25-web-slice-5-pos-submit-close-design.md)

## Global Constraints

- **No client-side state machine.** `derivePosPhase` is the only source of POS phase. `SubmitStatus` exists only to word the payment result screen; no phase decision ever reads it.
- **The server owns every figure.** Completed Sale totals, change, and progress counts come from response fields. Nothing is recomputed from the catalog.
- **Single-Intent Idempotency:** submit and close each hold one `request_id` from `newRequestId()` in a `useRef`. A retry reuses it; success or a session change clears it.
- **Every 409 refetches the session** (`isConflictError`), because the phase follows the server.
- **`NOTHING_TO_SUBMIT` is success**, never an error.
- **Takeaway only.** The drawer filters on `service_mode === "TAKEAWAY"`.
- **No end-to-end or browser integration tests.** Unit tests only, via `bun test`: pure logic plus `renderToString` output assertions.
- **Touch targets:** every touchable element keeps `min-h-[48px]` (and `min-w-[48px]` for icon buttons).
- **Press feedback:** buttons use `active:scale-[0.98]`.
- **Whole VND only:** `font-mono tabular-nums`, formatted with `formatVND` from `@/lib/utils`.
- **Banned elements:** no emojis, no em-dashes in UI copy, no neon glows, no pure black.
- **All generated model fields are optional.** Every read must tolerate `undefined`.
- **`pos-view.tsx` must not grow** past its current 390 lines.
- **The slice ends at a UAT gate** (Task 10). Completion is never self-certified.

---

## File Structure

**Create**

| File | Responsibility |
| :--- | :--- |
| `web/src/features/pos/api/use-close-session.ts` | `useCloseFlow`: close, Completed Sale state, closed-elsewhere recovery |
| `web/src/features/pos/api/use-close-session.test.ts` | Pure recovery rule, export smoke test |
| `web/src/features/pos/api/use-pos-session.test.ts` | Pointer drop rule |
| `web/src/features/pos/utils/pending-orders.ts` | Drawer rows: filter, derive, summarize, sort, age |
| `web/src/features/pos/utils/pending-orders.test.ts` | Tests for the above |
| `web/src/features/pos/utils/completed-sale.ts` | Completed Sale items, totals, payments, preparation summary |
| `web/src/features/pos/utils/completed-sale.test.ts` | Tests for the above |
| `web/src/features/pos/hooks/use-pos-hotkeys.ts` | F9 by phase, F4 drawer toggle |
| `web/src/features/pos/hooks/use-pos-hotkeys.test.ts` | `resolveF9Action` table |
| `web/src/features/pos/components/check-panel-actions.tsx` | Action buttons per phase |
| `web/src/features/pos/components/pending-orders-drawer.tsx` | Drawer and its entry button |
| `web/src/features/pos/components/pending-orders-drawer.test.tsx` | Render assertions |
| `web/src/features/pos/components/pending-order-row.tsx` | One drawer row |
| `web/src/features/pos/components/completed-sale-dialog.tsx` | Read-only Completed Sale dialog |
| `web/src/features/pos/components/completed-sale-dialog.test.tsx` | Render assertions |
| `web/src/features/pos/components/pos-menu-status.tsx` | Loading and menu-error screens moved out of `pos-view.tsx` |
| `web/src/features/pos/components/pos-error-toast.tsx` | Page-level error toast moved out of `pos-view.tsx` |
| `scripts/uat-advance-units.ts` | UAT scaffolding: advances a session's units to `FULFILLED` |
| `.superpowers/sdd/2026-09-25-web-slice-5-pos-submit-close/uat-handover.md` | UAT gate script |

**Modify**

| File | Change |
| :--- | :--- |
| `web/src/lib/unwrap.ts` (+ test) | `isConflictError` |
| `web/src/lib/error-messages.ts` (+ test) | Submit and closure codes |
| `web/src/features/pos/utils/phase.ts` (+ test) | New phases and helpers; `SubmitStatus` |
| `web/src/features/pos/api/use-pos.ts` | `useActiveSessions`, `useCompletedSale`, in-preparation polling |
| `web/src/features/pos/api/use-pos-session.ts` | `switchSession`, `shouldDropSessionPointer` |
| `web/src/features/pos/api/use-checkout.ts` (+ test) | `useSubmitOrder`, submit step, `submitOrder` action |
| `web/src/features/pos/components/payment-dialog.tsx` (+ test) | Submit outcome line |
| `web/src/features/pos/components/check-panel.tsx` (+ test) | New phases, progress, submit error |
| `web/src/features/pos/components/pos-view.tsx` (+ test) | Drawer, close flow, hotkeys hook, extractions |

`useSubmitOrder` lives in `use-checkout.ts` beside `useCommitDraft` and `usePayCash`, not in `use-pos.ts` as spec section 9 lists. All three write the same projection, and the checkout flow is their only caller.

---

## Task 1: Conflict detection and Vietnamese messages

**Files:**
- Modify: `web/src/lib/unwrap.ts`
- Modify: `web/src/lib/error-messages.ts`
- Test: `web/src/lib/unwrap.test.ts`, `web/src/lib/error-messages.test.ts`

**Interfaces:**
- Produces: `isConflictError(error: unknown): error is ApiError` from `@/lib/unwrap`; new `ERROR_MESSAGES` keys.

- [ ] **Step 1: Write the failing tests**

Append to `web/src/lib/unwrap.test.ts` (add `isConflictError` to its existing import from `./unwrap`):

```ts
describe("isConflictError", () => {
  it("is true for an HTTP 409 ApiError", () => {
    expect(isConflictError(new ApiError(409, "UNFULFILLED_PREPARATION_FOR_CLOSURE", "x"))).toBe(true);
  });

  it("is false for other statuses and for non-ApiErrors", () => {
    expect(isConflictError(new ApiError(404, "SERVICE_SESSION_NOT_FOUND", "x"))).toBe(false);
    expect(isConflictError(new ApiError(0, "NETWORK_ERROR", "x"))).toBe(false);
    expect(isConflictError(new Error("boom"))).toBe(false);
  });
});
```

Append inside the `describe("messageForError", ...)` block of `web/src/lib/error-messages.test.ts`:

```ts
  it("maps the submit and closure codes to Vietnamese messages", () => {
    const cases: Array<[string, string]> = [
      ["CHECK_NOT_SETTLED_FOR_SUBMISSION", "Đơn chưa thu đủ tiền, chưa thể gửi bếp."],
      ["UNFULFILLED_PREPARATION_FOR_CLOSURE", "Bếp chưa hoàn tất tất cả món."],
      ["UNSUBMITTED_WORK_FOR_CLOSURE", "Còn món chưa gửi bếp."],
      ["CHECK_NOT_SETTLED_FOR_CLOSURE", "Đơn chưa thu đủ tiền."],
      ["ORDER_REQUIRED_FOR_CLOSURE", "Đơn chưa có món nào được gửi bếp."],
      ["PENDING_REFUND_FOR_CLOSURE", "Đơn còn khoản hoàn tiền chưa xử lý. Vui lòng báo quản lý."],
      ["COMPLETED_SALE_NOT_FOUND", "Không tìm thấy hóa đơn hoàn tất."],
      ["NOTHING_TO_SUBMIT", "Đơn này đã được gửi bếp."],
    ];
    for (const [code, message] of cases) {
      expect(messageForError(new ApiError(409, code, code))).toBe(message);
    }
  });
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && bun test src/lib/unwrap.test.ts src/lib/error-messages.test.ts`
Expected: FAIL. `isConflictError` is not exported, and the new codes fall back to the raw code string.

- [ ] **Step 3: Implement**

Append to `web/src/lib/unwrap.ts`:

```ts
/**
 * A 409: the command raced the server's state. Callers refetch rather than
 * trust their cached projection, because the POS phase follows the server.
 */
export function isConflictError(error: unknown): error is ApiError {
  return error instanceof ApiError && error.status === 409;
}
```

In `web/src/lib/error-messages.ts`, add these entries after `REQUEST_CONFLICT`:

```ts
  NOTHING_TO_SUBMIT: "Đơn này đã được gửi bếp.",
  CHECK_NOT_SETTLED_FOR_SUBMISSION: "Đơn chưa thu đủ tiền, chưa thể gửi bếp.",
  UNFULFILLED_PREPARATION_FOR_CLOSURE: "Bếp chưa hoàn tất tất cả món.",
  UNSUBMITTED_WORK_FOR_CLOSURE: "Còn món chưa gửi bếp.",
  CHECK_NOT_SETTLED_FOR_CLOSURE: "Đơn chưa thu đủ tiền.",
  ORDER_REQUIRED_FOR_CLOSURE: "Đơn chưa có món nào được gửi bếp.",
  PENDING_REFUND_FOR_CLOSURE: "Đơn còn khoản hoàn tiền chưa xử lý. Vui lòng báo quản lý.",
  COMPLETED_SALE_NOT_FOUND: "Không tìm thấy hóa đơn hoàn tất.",
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && bun test src/lib/unwrap.test.ts src/lib/error-messages.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/unwrap.ts web/src/lib/unwrap.test.ts web/src/lib/error-messages.ts web/src/lib/error-messages.test.ts
git commit -m "feat(web): map submit and closure errors, detect conflicts"
```

---

## Task 2: Phase derivation for submit, preparation, and closure

**Files:**
- Modify: `web/src/features/pos/utils/phase.ts`
- Modify: `web/src/features/pos/components/check-panel.tsx:38` (one line)
- Modify: `web/src/features/pos/components/pos-view.tsx:28,255,310,323` (four lines)
- Test: `web/src/features/pos/utils/phase.test.ts`, `web/src/features/pos/components/check-panel.test.tsx`

**Interfaces:**
- Produces, all from `../utils/phase`:
  - `type PosPhase = "NO_SESSION" | "DRAFTING" | "AWAITING_PAYMENT" | "AWAITING_SUBMIT" | "IN_PREPARATION" | "READY_TO_CLOSE"`
  - `type SubmitStatus = "idle" | "submitting" | "submitted" | "failed"`
  - `interface PreparationProgress { done: number; total: number }`
  - `isTerminalUnit(unit: SalesPreparationUnitResponse): boolean`
  - `preparationProgress(session): PreparationProgress`
  - `hasUnsubmittedWork(session): boolean`
  - `isPostPaymentPhase(phase: PosPhase): boolean`

- [ ] **Step 1: Write the failing tests**

In `web/src/features/pos/utils/phase.test.ts`:

1. Extend the import:

```ts
import {
  derivePosPhase,
  listLiveChecks,
  selectOpenCheck,
  hasMultipleOpenChecks,
  findCheckById,
  isTerminalUnit,
  preparationProgress,
  hasUnsubmittedWork,
  isPostPaymentPhase,
} from "./phase";
```

2. Replace the test `"reports SETTLED once every Check is settled"` with:

```ts
  it("reports READY_TO_CLOSE for a settled session with nothing unsubmitted and no units", () => {
    const settled = session({
      checks: [{ id: "check-1", state: "SETTLED", balance_vnd: 0, total_applied_vnd: 47_000 }],
    });
    expect(derivePosPhase(settled)).toBe("READY_TO_CLOSE");
  });

  it("reports AWAITING_SUBMIT while a settled Check carries unsubmitted work", () => {
    const paid = session({
      checks: [
        {
          id: "check-1",
          state: "SETTLED",
          balance_vnd: 0,
          allocations: [{ id: "a1", submitted: false }],
        },
      ],
    });
    expect(derivePosPhase(paid)).toBe("AWAITING_SUBMIT");
  });

  it("reports IN_PREPARATION once submitted while a unit is still in the kitchen", () => {
    const submitted = session({
      checks: [{ id: "check-1", state: "SETTLED", balance_vnd: 0, allocations: [{ id: "a1", submitted: true }] }],
      preparation_units: [
        { id: "u1", state: "FULFILLED" },
        { id: "u2", state: "IN_PREPARATION" },
      ],
    });
    expect(derivePosPhase(submitted)).toBe("IN_PREPARATION");
  });

  it("reports READY_TO_CLOSE when every unit is terminal", () => {
    const done = session({
      checks: [{ id: "check-1", state: "SETTLED", balance_vnd: 0, allocations: [{ id: "a1", submitted: true }] }],
      preparation_units: [
        { id: "u1", state: "FULFILLED" },
        { id: "u2", state: "CANCELLED" },
        { id: "u3", state: "WASTED" },
      ],
    });
    expect(derivePosPhase(done)).toBe("READY_TO_CLOSE");
  });

  it("keeps IN_PREPARATION while the remake of a wasted unit is queued", () => {
    const remaking = session({
      checks: [{ id: "check-1", state: "SETTLED", balance_vnd: 0, allocations: [{ id: "a1", submitted: true }] }],
      preparation_units: [
        { id: "u1", state: "WASTED" },
        { id: "u2", state: "QUEUED", remake_of_preparation_unit_id: "u1" },
      ],
    });
    expect(derivePosPhase(remaking)).toBe("IN_PREPARATION");
  });
```

3. In the test `"ignores a Check that was merged away"`, change the expectation to `toBe("READY_TO_CLOSE")`.

4. Append new describe blocks at the end of the file:

```ts
describe("preparation helpers", () => {
  it("treats FULFILLED, CANCELLED and WASTED as terminal", () => {
    expect(isTerminalUnit({ state: "FULFILLED" })).toBe(true);
    expect(isTerminalUnit({ state: "CANCELLED" })).toBe(true);
    expect(isTerminalUnit({ state: "WASTED" })).toBe(true);
    expect(isTerminalUnit({ state: "QUEUED" })).toBe(false);
    expect(isTerminalUnit({ state: "READY" })).toBe(false);
    expect(isTerminalUnit({})).toBe(false);
  });

  it("counts terminal units over all units", () => {
    const s = session({
      preparation_units: [{ state: "FULFILLED" }, { state: "READY" }, { state: "WASTED" }],
    });
    expect(preparationProgress(s)).toEqual({ done: 2, total: 3 });
    expect(preparationProgress(null)).toEqual({ done: 0, total: 0 });
  });

  it("finds unsubmitted work only on live Checks", () => {
    const merged = session({
      checks: [
        { id: "c1", state: "MERGED", merged_into_check_id: "c2", allocations: [{ submitted: false }] },
        { id: "c2", state: "SETTLED", allocations: [{ submitted: true }] },
      ],
    });
    expect(hasUnsubmittedWork(merged)).toBe(false);
    expect(
      hasUnsubmittedWork(session({ checks: [{ id: "c1", state: "SETTLED", allocations: [{}] }] })),
    ).toBe(true);
  });

  it("groups the three phases that follow payment", () => {
    expect(isPostPaymentPhase("AWAITING_SUBMIT")).toBe(true);
    expect(isPostPaymentPhase("IN_PREPARATION")).toBe(true);
    expect(isPostPaymentPhase("READY_TO_CLOSE")).toBe(true);
    expect(isPostPaymentPhase("AWAITING_PAYMENT")).toBe(false);
    expect(isPostPaymentPhase("DRAFTING")).toBe(false);
    expect(isPostPaymentPhase("NO_SESSION")).toBe(false);
  });
});
```

In `web/src/features/pos/components/check-panel.test.tsx`, change `phase="SETTLED"` to `phase="AWAITING_SUBMIT"`. Leave the `"Mở ở Slice 5"` expectation for now; Task 6 replaces it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && bun test src/features/pos/utils/phase.test.ts`
Expected: FAIL. The new helpers are not exported, and `derivePosPhase` still answers `SETTLED`.

- [ ] **Step 3: Implement `phase.ts`**

In `web/src/features/pos/utils/phase.ts`:

1. Replace the imports and the `PosPhase` type:

```ts
import type {
  SalesCheckResponse,
  SalesPreparationUnitResponse,
  SalesServiceSessionResponse,
} from "@/api/generated/models";
```

```ts
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
```

2. Add these helpers above `derivePosPhase`:

```ts
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
```

3. Replace the tail of `derivePosPhase` (the line `return "SETTLED";`) with:

```ts
  if (hasUnsubmittedWork(session)) return "AWAITING_SUBMIT";

  const progress = preparationProgress(session);
  if (progress.done < progress.total) return "IN_PREPARATION";

  // Submit always creates units, so "no units" is unreachable by the domain.
  // Closure refuses with ORDER_REQUIRED_FOR_CLOSURE if it ever happens.
  return "READY_TO_CLOSE";
```

- [ ] **Step 4: Keep the consumers compiling**

In `web/src/features/pos/components/check-panel.tsx`, add `isPostPaymentPhase` to the import from `../utils/phase` and change line 38:

```ts
  const isSettled = isPostPaymentPhase(phase);
```

In `web/src/features/pos/components/pos-view.tsx`:
- line 28: `import { derivePosPhase, selectOpenCheck, isPostPaymentPhase } from "../utils/phase";`
- line 255: `if (isPostPaymentPhase(phase)) checkout.nextCustomer();`
- line 310: `disabled={!isShiftOpen || (phase !== "NO_SESSION" && phase !== "DRAFTING")}`
- line 323: `{phase === "AWAITING_PAYMENT" || isPostPaymentPhase(phase) ? (`

- [ ] **Step 5: Run the tests and the type check**

Run: `cd web && bun test src/features/pos && bunx tsc -b`
Expected: PASS, with no type errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/pos/utils/phase.ts web/src/features/pos/utils/phase.test.ts web/src/features/pos/components/check-panel.tsx web/src/features/pos/components/check-panel.test.tsx web/src/features/pos/components/pos-view.tsx
git commit -m "feat(web): derive submit, preparation and closure phases"
```

---

## Task 3: Session reads — active sessions, Completed Sale, switching

**Files:**
- Modify: `web/src/features/pos/api/use-pos.ts`
- Modify: `web/src/features/pos/api/use-pos-session.ts`
- Test: `web/src/features/pos/api/use-pos-session.test.ts` (create)

**Interfaces:**
- Consumes: `derivePosPhase` (Task 2).
- Produces:
  - `useActiveSessions(isDrawerOpen: boolean)`: react-query result whose `data` is `SalesServiceSessionResponse[]`, plus `dataUpdatedAt: number`.
  - `useCompletedSale(sessionId: string | null)`: react-query result whose `data` is `SalesCompletedSaleResponse`.
  - `ACTIVE_SESSIONS_POLL_MS = { open: 5_000, closed: 15_000 }`, `IN_PREPARATION_POLL_MS = 5_000`.
  - `useServiceSession` polls every 5 s while its session is `IN_PREPARATION`.
  - `shouldDropSessionPointer(session, isError): boolean`.
  - `PosSessionHandle.switchSession(id: string): void`.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/pos/api/use-pos-session.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { shouldDropSessionPointer, usePosSession } from "./use-pos-session";

describe("shouldDropSessionPointer", () => {
  it("keeps an active session", () => {
    expect(shouldDropSessionPointer({ id: "s1", state: "ACTIVE" }, false)).toBe(false);
  });

  it("keeps a closed session so its Completed Sale can be shown", () => {
    expect(shouldDropSessionPointer({ id: "s1", state: "CLOSED" }, false)).toBe(false);
  });

  it("keeps the pointer while the session is still loading", () => {
    expect(shouldDropSessionPointer(null, false)).toBe(false);
    expect(shouldDropSessionPointer({ id: "s1" }, false)).toBe(false);
  });

  it("drops the pointer on a read error or an unknown state", () => {
    expect(shouldDropSessionPointer(null, true)).toBe(true);
    expect(shouldDropSessionPointer({ id: "s1", state: "ABANDONED" }, false)).toBe(true);
  });
});

describe("usePosSession", () => {
  it("is exported", () => {
    expect(typeof usePosSession).toBe("function");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && bun test src/features/pos/api/use-pos-session.test.ts`
Expected: FAIL, because `shouldDropSessionPointer` is not exported.

- [ ] **Step 3: Implement `use-pos.ts` additions**

Extend the generated import in `web/src/features/pos/api/use-pos.ts`:

```ts
import {
  useGetSalesServiceSessionsId,
  useGetSalesServiceSessions,
  useGetSalesServiceSessionsIdCompletedSale,
  getGetSalesServiceSessionsIdQueryKey,
  usePostSalesServiceSessionsTakeaway,
  usePostSalesServiceSessionsIdDraftItems,
  usePatchSalesServiceSessionsIdDraftItemsItemIdQuantity,
  usePatchSalesServiceSessionsIdDraftItemsItemIdSize,
  usePatchSalesServiceSessionsIdDraftItemsItemIdModifiers,
  usePatchSalesServiceSessionsIdDraftItemsItemIdPreparationNote,
  useDeleteSalesServiceSessionsIdDraftItemsItemId,
} from "@/api/generated/endpoints/sales/sales";
import { derivePosPhase } from "../utils/phase";
```

Replace `useServiceSession` and add the two new reads after it:

```ts
/** The kitchen moves units; there is no push channel, so an in-progress order polls. */
export const IN_PREPARATION_POLL_MS = 5_000;

/**
 * Reads single Service Session with its active Order Draft projection.
 */
export function useServiceSession(sessionId: string | null) {
  return useGetSalesServiceSessionsId(sessionId ?? "", {
    query: {
      enabled: Boolean(sessionId),
      select: unwrap,
      staleTime: 5_000,
      refetchInterval: (query) =>
        derivePosPhase(query.state.data?.data) === "IN_PREPARATION"
          ? IN_PREPARATION_POLL_MS
          : false,
    },
  });
}

/** Fast while the cashier is looking at the list, slow while only the badge is. */
export const ACTIVE_SESSIONS_POLL_MS = { open: 5_000, closed: 15_000 } as const;

/**
 * Every ACTIVE Service Session with its full projection: the cashier's
 * open-tabs view behind the "Đơn đang chờ" drawer.
 */
export function useActiveSessions(isDrawerOpen: boolean) {
  return useGetSalesServiceSessions({
    query: {
      select: unwrap,
      refetchInterval: isDrawerOpen
        ? ACTIVE_SESSIONS_POLL_MS.open
        : ACTIVE_SESSIONS_POLL_MS.closed,
    },
  });
}

/**
 * The immutable Completed Sale of one closed Session. A Session that has not
 * closed answers 404, so this never retries.
 */
export function useCompletedSale(sessionId: string | null) {
  return useGetSalesServiceSessionsIdCompletedSale(sessionId ?? "", {
    query: {
      enabled: Boolean(sessionId),
      select: unwrap,
      retry: false,
      staleTime: Infinity,
    },
  });
}
```

- [ ] **Step 4: Implement `use-pos-session.ts` changes**

1. Add the pure rule above `usePosSession`:

```ts
/**
 * Whether the stored pointer no longer names a session this terminal can show.
 * A CLOSED session is kept: the close flow reads back its Completed Sale and
 * clears the pointer when the cashier dismisses it.
 */
export function shouldDropSessionPointer(
  session: SalesServiceSessionResponse | null | undefined,
  isError: boolean,
): boolean {
  if (isError) return true;
  const state = session?.state;
  return Boolean(state) && state !== "ACTIVE" && state !== "CLOSED";
}
```

2. Replace the drop effect with:

```ts
  // Drop the pointer when the Session is gone or in a state this terminal cannot show.
  React.useEffect(() => {
    if (shouldDropSessionPointer(session, isSessionError)) {
      writeStoredSessionId(null);
      activeSessionIdRef.current = null;
      // oxlint-disable-next-line react/set-state-in-effect
      setActiveSessionId(null);
    }
  }, [session, isSessionError]);
```

3. Add `switchSession` after `clearSession`:

```ts
  /** Reopens a session picked from the pending orders drawer. */
  const switchSession = React.useCallback((id: string) => {
    writeStoredSessionId(id);
    activeSessionIdRef.current = id;
    setActiveSessionId(id);
  }, []);
```

4. Add `switchSession: (id: string) => void;` to `PosSessionHandle`, and `switchSession,` to the returned object.

- [ ] **Step 5: Run the tests and the type check**

Run: `cd web && bun test src/features/pos && bunx tsc -b`
Expected: PASS, with no type errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/pos/api/use-pos.ts web/src/features/pos/api/use-pos-session.ts web/src/features/pos/api/use-pos-session.test.ts
git commit -m "feat(web): read active sessions and completed sales, switch sessions"
```

---

## Task 4: Automatic submit after payment

**Files:**
- Modify: `web/src/features/pos/api/use-checkout.ts`
- Modify: `web/src/features/pos/components/payment-dialog.tsx`
- Test: `web/src/features/pos/api/use-checkout.test.ts`, `web/src/features/pos/components/payment-dialog.test.tsx`

**Interfaces:**
- Consumes: `hasUnsubmittedWork`, `selectOpenCheck`, `SubmitStatus` (Task 2); `isConflictError` (Task 1).
- Produces:
  - `useSubmitOrder(sessionId: string)` returning `{ submitOrder(requestId?: string, targetSessionId?: string): Promise<SalesServiceSessionResponse> }`.
  - `SUBMIT_ALREADY_DONE_CODES: Set<string>`.
  - `CheckoutFlow` gains `submitStatus: SubmitStatus`, `submitError: string | null`, `submitOrder: () => Promise<void>`.
  - `PaymentDialogProps` gains `submitStatus: SubmitStatus` and `submitError: string | null`.

- [ ] **Step 1: Write the failing tests**

In `web/src/features/pos/api/use-checkout.test.ts`, extend the import and add:

```ts
import {
  COMMIT_FAILURE_CODES,
  SUBMIT_ALREADY_DONE_CODES,
  useCommitDraft,
  usePayCash,
  useSubmitOrder,
} from "./use-checkout";
```

```ts
describe("submit seam", () => {
  it("exports the submit hook", () => {
    expect(typeof useSubmitOrder).toBe("function");
  });

  /**
   * NOTHING_TO_SUBMIT means another tab, or an attempt whose response was
   * lost, already submitted. Treating it as failure would strand the cashier
   * on a retry button that can never succeed.
   */
  it("treats exactly NOTHING_TO_SUBMIT as already done", () => {
    expect([...SUBMIT_ALREADY_DONE_CODES]).toEqual(["NOTHING_TO_SUBMIT"]);
  });
});
```

In `web/src/features/pos/components/payment-dialog.test.tsx`, add `submitStatus: "idle" as const, submitError: null,` to `base`, then add:

```ts
  it("reports the order sent to the bar on the result screen", () => {
    const html = renderToString(
      <PaymentDialog {...base} changeDueVnd={3_000} submitStatus="submitted" />,
    );
    expect(html).toContain("Đã gửi bếp");
  });

  it("reports a submit in flight", () => {
    const html = renderToString(
      <PaymentDialog {...base} changeDueVnd={3_000} submitStatus="submitting" />,
    );
    expect(html).toContain("Đang gửi bếp");
  });

  it("keeps a submit failure apart from the payment", () => {
    const html = renderToString(
      <PaymentDialog
        {...base}
        changeDueVnd={3_000}
        submitStatus="failed"
        submitError="Không kết nối được máy chủ. Kiểm tra mạng nội bộ rồi thử lại."
      />,
    );
    expect(html).toContain("Tiền thối");
    expect(html).toContain("Đã thu tiền nhưng chưa gửi được bếp");
    expect(html).toContain("Không kết nối được máy chủ");
  });
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && bun test src/features/pos/api/use-checkout.test.ts src/features/pos/components/payment-dialog.test.tsx`
Expected: FAIL. `useSubmitOrder` and `SUBMIT_ALREADY_DONE_CODES` are missing, and the dialog has no submit line.

- [ ] **Step 3: Add `useSubmitOrder` to `use-checkout.ts`**

Replace the import block at the top of `web/src/features/pos/api/use-checkout.ts` with:

```ts
import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  usePostSalesServiceSessionsIdDraftCommit,
  usePostSalesChecksCheckIdPaymentsCash,
  usePostSalesServiceSessionsIdSubmit,
  getGetSalesServiceSessionsIdQueryKey,
  getGetSalesServiceSessionsQueryKey,
} from "@/api/generated/endpoints/sales/sales";
import { unwrap, ApiError, isConflictError } from "@/lib/unwrap";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { playSuccessChirp, playErrorBuzz } from "@/lib/sound";
import {
  selectOpenCheck,
  findCheckById,
  hasMultipleOpenChecks,
  hasUnsubmittedWork,
  type PosPhase,
  type SubmitStatus,
} from "../utils/phase";
import { latestPaymentChangeDue } from "../utils/payment";
import type {
  SalesPayCashCommand,
  SalesServiceSessionResponse,
} from "@/api/generated/models";
```

Add after `usePayCash`:

```ts
/**
 * Submits the committed round to the bar: an Order, its Order Items, and one
 * Preparation Unit per unit of quantity. A takeaway Session must be settled
 * first, so this always runs after the cash is taken.
 */
export function useSubmitOrder(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostSalesServiceSessionsIdSubmit();

  return {
    ...mutation,
    submitOrder: async (requestId?: string, targetSessionId?: string) => {
      const sid = targetSessionId || sessionId;
      const rid = requestId ?? newRequestId();
      const res = await mutation.mutateAsync({ id: sid, data: { request_id: rid } });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sid), res);
      void queryClient.invalidateQueries({ queryKey: getGetSalesServiceSessionsQueryKey() });
      return data;
    },
  };
}

/** Submit refusals that mean the work already reached the bar. */
export const SUBMIT_ALREADY_DONE_CODES = new Set(["NOTHING_TO_SUBMIT"]);
```

- [ ] **Step 4: Wire the submit step into `useCheckoutFlow`**

1. Add to `CheckoutFlow`:

```ts
  submitStatus: SubmitStatus;
  submitError: string | null;
  /** Submits a paid session that has not reached the bar ("Gửi bếp", F9). */
  submitOrder: () => Promise<void>;
```

2. Inside `useCheckoutFlow`, after `const { payCash } = ...`:

```ts
  const { submitOrder: postSubmit } = useSubmitOrder(activeSessionId ?? "");
  const queryClient = useQueryClient();
```

3. After `payRequestIdRef`:

```ts
  const submitRequestIdRef = React.useRef<string | null>(null);
  const [submitStatus, setSubmitStatus] = React.useState<SubmitStatus>("idle");
  const [submitError, setSubmitError] = React.useState<string | null>(null);

  // Another session carries other work: forget this one's submit attempt.
  React.useEffect(() => {
    submitRequestIdRef.current = null;
    // oxlint-disable-next-line react/set-state-in-effect
    setSubmitStatus("idle");
    setSubmitError(null);
  }, [activeSessionId]);

  const refetchSession = (sid: string) =>
    queryClient.invalidateQueries({ queryKey: getGetSalesServiceSessionsIdQueryKey(sid) });

  /**
   * Sends the committed round to the bar. Never throws: the money is already
   * recorded, so a failure is reported on its own and never as a payment
   * failure.
   */
  const runSubmit = async (sid: string): Promise<boolean> => {
    submitRequestIdRef.current = submitRequestIdRef.current ?? newRequestId();
    setSubmitStatus("submitting");
    setSubmitError(null);

    try {
      await postSubmit(submitRequestIdRef.current, sid);
    } catch (err) {
      if (err instanceof ApiError && SUBMIT_ALREADY_DONE_CODES.has(err.code)) {
        await refetchSession(sid);
      } else {
        if (isConflictError(err)) void refetchSession(sid);
        playErrorBuzz();
        setSubmitStatus("failed");
        setSubmitError(messageForError(err));
        return false;
      }
    }

    submitRequestIdRef.current = null;
    setSubmitStatus("submitted");
    return true;
  };

  const submitOrder = async () => {
    if (!activeSessionId || phase !== "AWAITING_SUBMIT" || submitStatus === "submitting") return;
    if (await runSubmit(activeSessionId)) playSuccessChirp();
  };
```

4. In `confirmPayment`, directly after `playSuccessChirp();`:

```ts
      // Takeaway submits once every Check is settled.
      if (!selectOpenCheck(paid) && hasUnsubmittedWork(paid)) {
        await runSubmit(activeSessionId);
      }
```

5. Replace `nextCustomer`:

```ts
  const nextCustomer = () => {
    commitRequestIdRef.current = null;
    payRequestIdRef.current = null;
    submitRequestIdRef.current = null;
    setSubmitStatus("idle");
    setSubmitError(null);
    clearSession();
  };
```

6. Add `submitStatus, submitError, submitOrder,` to the returned object.

- [ ] **Step 5: Add the submit line to the payment dialog**

In `web/src/features/pos/components/payment-dialog.tsx`:

1. Imports:

```ts
import { X, Banknote, AlertCircle, CheckCircle2, ChefHat, Loader2 } from "lucide-react";
import type { SubmitStatus } from "../utils/phase";
```

2. Add to `PaymentDialogProps`:

```ts
  /** The automatic submit that follows payment; shown on the result screen only. */
  submitStatus: SubmitStatus;
  submitError: string | null;
```

3. Destructure `submitStatus, submitError,` in the component signature.

4. In the result screen, after the `Tiền thối` block (inside `<div className="p-6 space-y-4 text-center">`), add:

```tsx
            <SubmitOutcome status={submitStatus} error={submitError} />
```

5. Add the component at the bottom of the file:

```tsx
function SubmitOutcome({ status, error }: { status: SubmitStatus; error: string | null }) {
  if (status === "submitting") {
    return (
      <p className="flex items-center justify-center gap-2 text-xs font-semibold text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        Đang gửi bếp...
      </p>
    );
  }
  if (status === "submitted") {
    return (
      <p className="flex items-center justify-center gap-2 text-xs font-semibold text-emerald-700 dark:text-emerald-300">
        <ChefHat className="h-4 w-4" />
        Đã gửi bếp
      </p>
    );
  }
  if (status === "failed") {
    return (
      <div
        role="alert"
        className="flex items-start gap-2 rounded-xl bg-amber-100 text-amber-900 dark:bg-amber-950/60 dark:text-amber-200 px-3 py-2.5 text-left text-xs font-semibold"
      >
        <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
        <span>
          Đã thu tiền nhưng chưa gửi được bếp. {error} Bấm "Gửi bếp" ở hóa đơn để thử lại.
        </span>
      </div>
    );
  }
  return null;
}
```

6. In `pos-view.tsx`, pass the new props to `<PaymentDialog>` so it keeps compiling:

```tsx
        submitStatus={checkout.submitStatus}
        submitError={checkout.submitError}
```

In `pos-view.test.tsx`, add `submitStatus: "idle", submitError: null, submitOrder: async () => {},` to the `useCheckoutFlow` mock.

- [ ] **Step 6: Run the tests and the type check**

Run: `cd web && bun test src/features/pos && bunx tsc -b`
Expected: PASS, with no type errors.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/pos/api/use-checkout.ts web/src/features/pos/api/use-checkout.test.ts web/src/features/pos/components/payment-dialog.tsx web/src/features/pos/components/payment-dialog.test.tsx web/src/features/pos/components/pos-view.tsx web/src/features/pos/components/pos-view.test.tsx
git commit -m "feat(web): submit takeaway orders to the bar after payment"
```

---

## Task 5: Session closure and the Completed Sale dialog

**Files:**
- Create: `web/src/features/pos/utils/completed-sale.ts`, `web/src/features/pos/utils/completed-sale.test.ts`
- Create: `web/src/features/pos/api/use-close-session.ts`, `web/src/features/pos/api/use-close-session.test.ts`
- Create: `web/src/features/pos/components/completed-sale-dialog.tsx`, `web/src/features/pos/components/completed-sale-dialog.test.tsx`

**Interfaces:**
- Consumes: `useCompletedSale` (Task 3); `derivePosPhase` (Task 2); `isConflictError` (Task 1).
- Produces:
  - `completedSaleItems(sale): SalesChargeAllocationResponse[]`
  - `completedSaleTotals(sale): { chargeVnd: number; receivedVnd: number }`
  - `completedSalePayments(sale): SalesPaymentResponse[]`
  - `summarizePreparation(units?: SalesPreparationUnitResponse[]): string`
  - `formatCompletedAt(iso?: string): string`
  - `recoverySessionId(session, hasClosedHere: boolean): string | null`
  - `useCloseFlow(options: CloseFlowOptions): CloseFlow` with `CloseFlow = { isClosing: boolean; completedSale: SalesCompletedSaleResponse | null; closeSession: () => Promise<void>; dismissCompletedSale: () => void }` and `CloseFlowOptions = { activeSessionId: string | null; session: SalesServiceSessionResponse | null; clearSession: () => void; onError: (message: string) => void }`
  - `<CompletedSaleDialog sale={SalesCompletedSaleResponse | null} onDone={() => void} />`

- [ ] **Step 1: Write the failing tests**

Create `web/src/features/pos/utils/completed-sale.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import {
  completedSaleItems,
  completedSaleTotals,
  completedSalePayments,
  summarizePreparation,
  formatCompletedAt,
} from "./completed-sale";
import type { SalesCompletedSaleResponse } from "@/api/generated/models";

const sale: SalesCompletedSaleResponse = {
  id: "sale-1",
  service_number: "012",
  checks: [
    {
      id: "c1",
      charge_vnd: 47_000,
      effective_received_vnd: 47_000,
      allocations: [{ id: "a1", name: "Cà phê sữa đá" }],
      payments: [
        { id: "p1", method: "CASH", cash_tendered_vnd: 50_000, change_due_vnd: 3_000 },
        { id: "p0", method: "CASH", void: {} },
      ],
    },
    {
      id: "c2",
      charge_vnd: 30_000,
      effective_received_vnd: 30_000,
      allocations: [{ id: "a2", name: "Bạc xỉu" }],
      payments: [],
    },
  ],
};

describe("completed sale readers", () => {
  it("lists the items of every Check", () => {
    expect(completedSaleItems(sale).map((a) => a.name)).toEqual(["Cà phê sữa đá", "Bạc xỉu"]);
  });

  it("sums the server's charge and received figures", () => {
    expect(completedSaleTotals(sale)).toEqual({ chargeVnd: 77_000, receivedVnd: 77_000 });
  });

  it("drops voided payments", () => {
    expect(completedSalePayments(sale).map((p) => p.id)).toEqual(["p1"]);
  });

  it("tolerates a sale with no checks", () => {
    expect(completedSaleItems({})).toEqual([]);
    expect(completedSaleTotals({})).toEqual({ chargeVnd: 0, receivedVnd: 0 });
  });
});

describe("summarizePreparation", () => {
  it("counts each terminal outcome in a fixed order", () => {
    expect(
      summarizePreparation([
        { state: "FULFILLED" },
        { state: "CANCELLED" },
        { state: "FULFILLED" },
        { state: "WASTED" },
      ]),
    ).toBe("2 món đã giao · 1 món hủy · 1 món hỏng");
  });

  it("omits outcomes that did not happen", () => {
    expect(summarizePreparation([{ state: "FULFILLED" }])).toBe("1 món đã giao");
  });

  it("says so when there is nothing", () => {
    expect(summarizePreparation([])).toBe("Không có món pha chế");
    expect(summarizePreparation(undefined)).toBe("Không có món pha chế");
  });
});

describe("formatCompletedAt", () => {
  it("returns an empty string for a missing or invalid time", () => {
    expect(formatCompletedAt(undefined)).toBe("");
    expect(formatCompletedAt("not a date")).toBe("");
  });

  it("formats a valid time", () => {
    expect(formatCompletedAt("2026-09-25T03:04:00Z")).not.toBe("");
  });
});
```

Create `web/src/features/pos/api/use-close-session.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { recoverySessionId, useCloseFlow } from "./use-close-session";

describe("recoverySessionId", () => {
  it("reads back a session that closed somewhere else", () => {
    expect(recoverySessionId({ id: "s1", state: "CLOSED" }, false)).toBe("s1");
  });

  it("does not read back a session this terminal just closed", () => {
    expect(recoverySessionId({ id: "s1", state: "CLOSED" }, true)).toBeNull();
  });

  it("ignores active and missing sessions", () => {
    expect(recoverySessionId({ id: "s1", state: "ACTIVE" }, false)).toBeNull();
    expect(recoverySessionId(null, false)).toBeNull();
  });
});

describe("useCloseFlow", () => {
  it("is exported", () => {
    expect(typeof useCloseFlow).toBe("function");
  });
});
```

Create `web/src/features/pos/components/completed-sale-dialog.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { CompletedSaleDialog } from "./completed-sale-dialog";
import type { SalesCompletedSaleResponse } from "@/api/generated/models";

const sale: SalesCompletedSaleResponse = {
  id: "sale-1",
  service_number: "012",
  completed_at: "2026-09-25T03:04:00Z",
  completed_by_display_name: "Thu Ngan A",
  checks: [
    {
      id: "c1",
      charge_vnd: 47_000,
      effective_received_vnd: 47_000,
      allocations: [
        { id: "a1", name: "Cà phê sữa đá", allocated_quantity: 2, amount_vnd: 47_000 },
      ],
      payments: [{ id: "p1", method: "CASH", cash_tendered_vnd: 50_000, change_due_vnd: 3_000 }],
    },
  ],
  preparation_units: [{ state: "FULFILLED" }, { state: "FULFILLED" }],
};

describe("CompletedSaleDialog", () => {
  it("renders nothing without a sale", () => {
    expect(renderToString(<CompletedSaleDialog sale={null} onDone={() => {}} />)).toBe("");
  });

  it("shows the frozen record of the sale", () => {
    const html = renderToString(<CompletedSaleDialog sale={sale} onDone={() => {}} />);
    expect(html).toContain("Đơn #012 đã hoàn tất");
    expect(html).toContain("Thu Ngan A");
    expect(html).toContain("Cà phê sữa đá");
    expect(html).toContain("47.000");
    expect(html).toContain("50.000");
    expect(html).toContain("3.000");
    expect(html).toContain("2 món đã giao");
    expect(html).toContain("Xong (Enter)");
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && bun test src/features/pos/utils/completed-sale.test.ts src/features/pos/api/use-close-session.test.ts src/features/pos/components/completed-sale-dialog.test.tsx`
Expected: FAIL, because the modules do not exist.

- [ ] **Step 3: Implement `utils/completed-sale.ts`**

```ts
import type {
  SalesChargeAllocationResponse,
  SalesCompletedSaleResponse,
  SalesPaymentResponse,
  SalesPreparationUnitResponse,
} from "@/api/generated/models";

/**
 * Readers over the immutable Completed Sale. Every figure is the server's
 * frozen record; nothing here recomputes a price.
 */

export function completedSaleItems(sale: SalesCompletedSaleResponse): SalesChargeAllocationResponse[] {
  return (sale.checks ?? []).flatMap((check) => check.allocations ?? []);
}

export function completedSaleTotals(sale: SalesCompletedSaleResponse): {
  chargeVnd: number;
  receivedVnd: number;
} {
  return (sale.checks ?? []).reduce(
    (totals, check) => ({
      chargeVnd: totals.chargeVnd + (check.charge_vnd ?? 0),
      receivedVnd: totals.receivedVnd + (check.effective_received_vnd ?? 0),
    }),
    { chargeVnd: 0, receivedVnd: 0 },
  );
}

export function completedSalePayments(sale: SalesCompletedSaleResponse): SalesPaymentResponse[] {
  return (sale.checks ?? [])
    .flatMap((check) => check.payments ?? [])
    .filter((payment) => !payment.void);
}

const OUTCOMES: ReadonlyArray<[state: string, label: string]> = [
  ["FULFILLED", "đã giao"],
  ["CANCELLED", "hủy"],
  ["WASTED", "hỏng"],
];

export function summarizePreparation(units: SalesPreparationUnitResponse[] | undefined): string {
  const parts = OUTCOMES.flatMap(([state, label]) => {
    const count = (units ?? []).filter((unit) => unit.state === state).length;
    return count > 0 ? [`${count} món ${label}`] : [];
  });
  return parts.length > 0 ? parts.join(" · ") : "Không có món pha chế";
}

const COMPLETED_AT_FORMAT = new Intl.DateTimeFormat("vi-VN", {
  hour: "2-digit",
  minute: "2-digit",
  day: "2-digit",
  month: "2-digit",
  year: "numeric",
});

export function formatCompletedAt(iso: string | undefined): string {
  if (!iso) return "";
  const at = new Date(iso);
  return Number.isNaN(at.getTime()) ? "" : COMPLETED_AT_FORMAT.format(at);
}
```

- [ ] **Step 4: Implement `api/use-close-session.ts`**

```ts
import * as React from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  usePostSalesServiceSessionsIdClose,
  getGetSalesServiceSessionsIdQueryKey,
  getGetSalesServiceSessionsQueryKey,
} from "@/api/generated/endpoints/sales/sales";
import { unwrap, isConflictError } from "@/lib/unwrap";
import { newRequestId } from "@/lib/command";
import { messageForError } from "@/lib/error-messages";
import { playSuccessChirp, playErrorBuzz } from "@/lib/sound";
import { useCompletedSale } from "./use-pos";
import { derivePosPhase } from "../utils/phase";
import type {
  SalesCompletedSaleResponse,
  SalesServiceSessionResponse,
} from "@/api/generated/models";

export interface CloseFlowOptions {
  activeSessionId: string | null;
  session: SalesServiceSessionResponse | null;
  clearSession: () => void;
  /** Routes closure refusals to the page-level toast. */
  onError: (message: string) => void;
}

export interface CloseFlow {
  isClosing: boolean;
  /** Non-null while the Completed Sale dialog is showing. */
  completedSale: SalesCompletedSaleResponse | null;
  closeSession: () => Promise<void>;
  dismissCompletedSale: () => void;
}

/**
 * The session whose Completed Sale must be read back because it closed
 * somewhere else, for example in a second tab. A sale this terminal closed
 * already arrived in the close response.
 */
export function recoverySessionId(
  session: SalesServiceSessionResponse | null | undefined,
  hasClosedHere: boolean,
): string | null {
  if (hasClosedHere) return null;
  return session?.state === "CLOSED" ? (session.id ?? null) : null;
}

/**
 * Freezes a finished Service Session into its Completed Sale and shows it.
 * Closure is idempotent on the server, so a retry after a lost response
 * returns the same sale.
 */
export function useCloseFlow({
  activeSessionId,
  session,
  clearSession,
  onError,
}: CloseFlowOptions): CloseFlow {
  const queryClient = useQueryClient();
  const mutation = usePostSalesServiceSessionsIdClose();

  const requestIdRef = React.useRef<string | null>(null);
  const [isClosing, setIsClosing] = React.useState(false);
  const [closedSale, setClosedSale] = React.useState<SalesCompletedSaleResponse | null>(null);

  const recoveryId = recoverySessionId(session, closedSale !== null);
  const recovered = useCompletedSale(recoveryId);

  // A closed session whose Completed Sale cannot be read has nothing to show.
  React.useEffect(() => {
    if (recoveryId && recovered.isError) clearSession();
  }, [recoveryId, recovered.isError, clearSession]);

  const closeSession = async () => {
    if (!activeSessionId || isClosing) return;
    if (derivePosPhase(session) !== "READY_TO_CLOSE") return;

    requestIdRef.current = requestIdRef.current ?? newRequestId();
    setIsClosing(true);

    try {
      const res = await mutation.mutateAsync({
        id: activeSessionId,
        data: { request_id: requestIdRef.current },
      });
      setClosedSale(unwrap(res));
      requestIdRef.current = null;
      void queryClient.invalidateQueries({ queryKey: getGetSalesServiceSessionsQueryKey() });
      playSuccessChirp();
    } catch (err) {
      playErrorBuzz();
      onError(messageForError(err));
      if (isConflictError(err)) {
        void queryClient.invalidateQueries({
          queryKey: getGetSalesServiceSessionsIdQueryKey(activeSessionId),
        });
      }
    } finally {
      setIsClosing(false);
    }
  };

  const dismissCompletedSale = () => {
    const sid = activeSessionId;
    setClosedSale(null);
    requestIdRef.current = null;
    clearSession();
    if (sid) queryClient.removeQueries({ queryKey: getGetSalesServiceSessionsIdQueryKey(sid) });
  };

  return {
    isClosing,
    completedSale: closedSale ?? recovered.data ?? null,
    closeSession,
    dismissCompletedSale,
  };
}
```

- [ ] **Step 5: Implement `components/completed-sale-dialog.tsx`**

```tsx
import { CheckCircle2, ChefHat } from "lucide-react";
import { useHotkeys } from "react-hotkeys-hook";
import type { SalesCompletedSaleResponse } from "@/api/generated/models";
import { formatVND } from "@/lib/utils";
import { playSuccessChirp } from "@/lib/sound";
import {
  completedSaleItems,
  completedSaleTotals,
  completedSalePayments,
  summarizePreparation,
  formatCompletedAt,
} from "../utils/completed-sale";

export interface CompletedSaleDialogProps {
  sale: SalesCompletedSaleResponse | null;
  onDone: () => void;
}

/** The immutable record of a closed sale, shown once before the next customer. */
export function CompletedSaleDialog({ sale, onDone }: CompletedSaleDialogProps) {
  const handleDone = () => {
    playSuccessChirp();
    onDone();
  };

  useHotkeys(
    "enter, f9, escape",
    (event) => {
      event.preventDefault();
      handleDone();
    },
    { enabled: sale !== null, enableOnFormTags: true },
  );

  if (!sale) return null;

  const items = completedSaleItems(sale);
  const totals = completedSaleTotals(sale);
  const payments = completedSalePayments(sale);
  const byline = [formatCompletedAt(sale.completed_at), sale.completed_by_display_name]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-xs animate-in fade-in duration-150">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="completed-sale-title"
        className="flex flex-col w-full max-w-md max-h-[90vh] rounded-2xl border border-border bg-card shadow-2xl overflow-hidden animate-in fade-in zoom-in-95 duration-150"
      >
        <div className="flex items-center gap-3 border-b border-border p-4 bg-muted/20 shrink-0">
          <div className="h-10 w-10 rounded-xl bg-emerald-100 text-emerald-700 dark:bg-emerald-950/60 dark:text-emerald-300 flex items-center justify-center">
            <CheckCircle2 className="h-5 w-5" />
          </div>
          <div>
            <h3 id="completed-sale-title" className="text-base font-bold text-foreground">
              {`Đơn #${sale.service_number ?? ""} đã hoàn tất`}
            </h3>
            {byline && <p className="text-2xs text-muted-foreground">{byline}</p>}
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-4 space-y-2">
          {items.map((item) => (
            <div key={item.id} className="flex items-baseline justify-between gap-2 text-sm">
              <span className="text-foreground">
                <span className="font-mono tabular-nums text-muted-foreground mr-2">
                  {item.allocated_quantity ?? 0}x
                </span>
                {item.name}
              </span>
              <span className="font-mono tabular-nums font-semibold text-foreground shrink-0">
                {formatVND(item.amount_vnd ?? 0)}
              </span>
            </div>
          ))}
        </div>

        <div className="border-t border-border p-4 space-y-1.5 shrink-0">
          <div className="flex items-baseline justify-between">
            <span className="text-sm font-bold text-foreground">Tổng cộng</span>
            <span className="font-mono text-xl font-bold text-primary tabular-nums">
              {formatVND(totals.chargeVnd)}
            </span>
          </div>
          <div className="flex items-baseline justify-between text-xs text-muted-foreground">
            <span>Đã nhận</span>
            <span className="font-mono tabular-nums font-semibold text-foreground">
              {formatVND(totals.receivedVnd)}
            </span>
          </div>
          {payments.map((payment) => (
            <div
              key={payment.id}
              className="flex items-baseline justify-between text-xs text-muted-foreground"
            >
              <span>{payment.method === "CASH" ? "Tiền mặt" : "Chuyển khoản"}</span>
              <span className="font-mono tabular-nums">
                {`Khách đưa ${formatVND(payment.cash_tendered_vnd ?? payment.applied_amount_vnd ?? 0)} · Thối ${formatVND(payment.change_due_vnd ?? 0)}`}
              </span>
            </div>
          ))}
          <p className="flex items-center gap-1.5 pt-1 text-xs text-muted-foreground">
            <ChefHat className="h-3.5 w-3.5" />
            {summarizePreparation(sale.preparation_units)}
          </p>
        </div>

        <div className="border-t border-border bg-muted/20 p-4 shrink-0">
          <button
            type="button"
            onClick={handleDone}
            className="min-h-[48px] w-full rounded-xl bg-primary text-sm font-bold text-primary-foreground select-none active:scale-[0.98] transition"
          >
            Xong (Enter)
          </button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 6: Run the tests and the type check**

Run: `cd web && bun test src/features/pos && bunx tsc -b`
Expected: PASS, with no type errors.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/pos/utils/completed-sale.ts web/src/features/pos/utils/completed-sale.test.ts web/src/features/pos/api/use-close-session.ts web/src/features/pos/api/use-close-session.test.ts web/src/features/pos/components/completed-sale-dialog.tsx web/src/features/pos/components/completed-sale-dialog.test.tsx
git commit -m "feat(web): close service sessions and show the completed sale"
```

---

## Task 6: Check panel for the post-payment phases

**Files:**
- Create: `web/src/features/pos/components/check-panel-actions.tsx`
- Modify: `web/src/features/pos/components/check-panel.tsx`
- Modify: `web/src/features/pos/components/pos-view.tsx` (the `<CheckPanel>` props only)
- Test: `web/src/features/pos/components/check-panel.test.tsx`

**Interfaces:**
- Consumes: `isPostPaymentPhase`, `preparationProgress` (Task 2); `CheckoutFlow.submitOrder/submitStatus/submitError` (Task 4).
- Produces: `CheckPanelProps` gains `onSubmit: () => void`, `onClose: () => void`, `isSubmitting: boolean`, `isClosing: boolean`, `submitError: string | null`.

- [ ] **Step 1: Write the failing tests**

Replace `web/src/features/pos/components/check-panel.test.tsx` entirely:

```tsx
import { describe, it, expect } from "bun:test";
import { renderToString } from "react-dom/server";
import { CheckPanel, type CheckPanelProps } from "./check-panel";
import type { SalesServiceSessionResponse } from "@/api/generated/models";

function session(
  checks: SalesServiceSessionResponse["checks"],
  preparation_units: SalesServiceSessionResponse["preparation_units"] = [],
): SalesServiceSessionResponse {
  return {
    id: "session-1",
    service_number: "007",
    service_mode: "TAKEAWAY",
    state: "ACTIVE",
    sales_shift_id: "shift-1",
    tables: [],
    checks,
    orders: [],
    preparation_units,
  };
}

const openCheck = {
  id: "check-1",
  state: "OPEN",
  charge_vnd: 47_000,
  balance_vnd: 47_000,
  created_at: "2026-09-24T01:00:00Z",
  payments: [],
  allocations: [
    {
      id: "alloc-1",
      name: "Cà phê sữa đá",
      size_name: "Vừa (M)",
      allocated_quantity: 2,
      amount_vnd: 47_000,
      modifiers: [],
      submitted: false,
    },
  ],
};

const settledCheck = (submitted: boolean) => ({
  ...openCheck,
  state: "SETTLED",
  balance_vnd: 0,
  total_applied_vnd: 47_000,
  allocations: openCheck.allocations.map((a) => ({ ...a, submitted })),
  payments: [
    {
      id: "pay-1",
      method: "CASH",
      applied_amount_vnd: 47_000,
      cash_tendered_vnd: 50_000,
      change_due_vnd: 3_000,
      received_at: "2026-09-24T02:00:00Z",
    },
  ],
});

const handlers = {
  onCollect: () => {},
  onSubmit: () => {},
  onClose: () => {},
  onNextCustomer: () => {},
  isSubmitting: false,
  isClosing: false,
  submitError: null,
};

function render(props: Pick<CheckPanelProps, "session" | "phase"> & Partial<CheckPanelProps>) {
  return renderToString(<CheckPanel {...handlers} {...props} />);
}

describe("CheckPanel", () => {
  it("lists the committed items and the outstanding balance", () => {
    const html = render({ session: session([openCheck]), phase: "AWAITING_PAYMENT" });
    expect(html).toContain("Đã chốt");
    expect(html).toContain("Cà phê sữa đá");
    expect(html).toContain("Vừa (M)");
    expect(html).toContain("Còn phải thu");
    expect(html).toContain("47.000");
    expect(html).toContain("Thu tiền (F9)");
  });

  it("refuses to collect when more than one Check is open", () => {
    const html = render({
      session: session([openCheck, { ...openCheck, id: "check-2" }]),
      phase: "AWAITING_PAYMENT",
    });
    expect(html).toContain("nhiều hóa đơn");
    expect(html).toContain("disabled");
  });

  it("offers Gửi bếp when paid but not submitted", () => {
    const html = render({ session: session([settledCheck(false)]), phase: "AWAITING_SUBMIT" });
    expect(html).toContain("Chờ gửi bếp");
    expect(html).toContain("Tiền thối");
    expect(html).toContain("3.000");
    expect(html).toContain("Gửi bếp (F9)");
    expect(html).toContain("Khách tiếp theo");
    expect(html).not.toContain("Mở ở Slice 5");
  });

  it("shows the submit failure beside the retry", () => {
    const html = render({
      session: session([settledCheck(false)]),
      phase: "AWAITING_SUBMIT",
      submitError: "Không kết nối được máy chủ.",
    });
    expect(html).toContain("Không kết nối được máy chủ.");
  });

  it("shows progress and a disabled Hoàn tất while the kitchen works", () => {
    const html = render({
      session: session([settledCheck(true)], [{ state: "FULFILLED" }, { state: "QUEUED" }]),
      phase: "IN_PREPARATION",
    });
    expect(html).toContain("Đang pha chế");
    expect(html).toContain("Đã xong 1/2 món");
    expect(html).toContain("Chờ bếp hoàn tất");
    expect(html).toContain("Khách tiếp theo (F9)");
  });

  it("offers Hoàn tất once every unit is terminal", () => {
    const html = render({
      session: session([settledCheck(true)], [{ state: "FULFILLED" }, { state: "FULFILLED" }]),
      phase: "READY_TO_CLOSE",
    });
    expect(html).toContain("Sẵn sàng hoàn tất");
    expect(html).toContain("Đã xong 2/2 món");
    expect(html).toContain("Hoàn tất (F9)");
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && bun test src/features/pos/components/check-panel.test.tsx`
Expected: FAIL. The panel still shows "Mở ở Slice 5" and has no progress row.

- [ ] **Step 3: Create `check-panel-actions.tsx`**

```tsx
import type { ReactNode } from "react";
import { CheckCircle2, ChefHat } from "lucide-react";
import type { PosPhase } from "../utils/phase";

export interface CheckPanelActionsProps {
  phase: PosPhase;
  canCollect: boolean;
  isSubmitting: boolean;
  isClosing: boolean;
  onCollect: () => void;
  onSubmit: () => void;
  onClose: () => void;
  onNextCustomer: () => void;
}

const PRIMARY =
  "min-h-[48px] h-12 w-full rounded-xl bg-primary text-sm font-bold text-primary-foreground disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2 select-none active:scale-[0.98] transition";
const SECONDARY =
  "min-h-[48px] h-12 w-full rounded-xl border border-border bg-card text-sm font-bold text-foreground hover:bg-muted flex items-center justify-center gap-2 select-none active:scale-[0.98] transition";

function Bar({ children }: { children: ReactNode }) {
  return <div className="border-t border-border bg-muted/20 p-4 space-y-2.5 shrink-0">{children}</div>;
}

/**
 * One primary action per phase, labelled with the key that fires it.
 * F9 mapping lives in hooks/use-pos-hotkeys.ts and must match these labels.
 */
export function CheckPanelActions({
  phase,
  canCollect,
  isSubmitting,
  isClosing,
  onCollect,
  onSubmit,
  onClose,
  onNextCustomer,
}: CheckPanelActionsProps) {
  switch (phase) {
    case "AWAITING_PAYMENT":
      return (
        <Bar>
          <button type="button" onClick={onCollect} disabled={!canCollect} className={PRIMARY}>
            Thu tiền (F9)
          </button>
        </Bar>
      );
    case "AWAITING_SUBMIT":
      return (
        <Bar>
          <button type="button" onClick={onSubmit} disabled={isSubmitting} className={PRIMARY}>
            <ChefHat className="h-4 w-4" />
            {isSubmitting ? "Đang gửi bếp..." : "Gửi bếp (F9)"}
          </button>
          <button type="button" onClick={onNextCustomer} className={SECONDARY}>
            Khách tiếp theo
          </button>
        </Bar>
      );
    case "IN_PREPARATION":
      return (
        <Bar>
          <button type="button" onClick={onNextCustomer} className={SECONDARY}>
            <CheckCircle2 className="h-4 w-4" />
            Khách tiếp theo (F9)
          </button>
          <button type="button" disabled className={`${PRIMARY} flex-col gap-0`}>
            <span>Hoàn tất</span>
            <span className="text-2xs font-normal">Chờ bếp hoàn tất</span>
          </button>
        </Bar>
      );
    case "READY_TO_CLOSE":
      return (
        <Bar>
          <button type="button" onClick={onClose} disabled={isClosing} className={PRIMARY}>
            <CheckCircle2 className="h-4 w-4" />
            {isClosing ? "Đang hoàn tất..." : "Hoàn tất (F9)"}
          </button>
          <button type="button" onClick={onNextCustomer} className={SECONDARY}>
            Khách tiếp theo
          </button>
        </Bar>
      );
    default:
      return null;
  }
}
```

- [ ] **Step 4: Rewrite `check-panel.tsx`**

Replace the whole file:

```tsx
import { Receipt, AlertTriangle, AlertCircle, ChefHat } from "lucide-react";
import type { SalesServiceSessionResponse } from "@/api/generated/models";
import {
  listLiveChecks,
  selectOpenCheck,
  hasMultipleOpenChecks,
  isPostPaymentPhase,
  preparationProgress,
  type PosPhase,
} from "../utils/phase";
import { latestPaymentChangeDue } from "../utils/payment";
import { formatVND } from "@/lib/utils";
import { CheckPanelActions } from "./check-panel-actions";

export interface CheckPanelProps {
  session: SalesServiceSessionResponse | null;
  phase: PosPhase;
  onCollect: () => void;
  onSubmit: () => void;
  onClose: () => void;
  onNextCustomer: () => void;
  isSubmitting: boolean;
  isClosing: boolean;
  submitError: string | null;
  className?: string;
}

const HEADINGS: Partial<Record<PosPhase, { badge: string; caption: string }>> = {
  AWAITING_PAYMENT: { badge: "Đã chốt", caption: "Đơn đã chốt giá, chờ thu tiền" },
  AWAITING_SUBMIT: { badge: "Chờ gửi bếp", caption: "Đã thu tiền, chưa gửi bếp" },
  IN_PREPARATION: { badge: "Đang pha chế", caption: "Bếp đang làm món" },
  READY_TO_CLOSE: { badge: "Sẵn sàng hoàn tất", caption: "Bếp đã xong, có thể hoàn tất đơn" },
};

/**
 * The Order Bill once the draft is committed: a read-only Check, then the
 * kitchen's progress, then closure.
 *
 * Every amount here is the server's frozen snapshot. Nothing on this panel is
 * recomputed from the catalog, because the customer is charged what the Check
 * says and not what the menu says today.
 */
export function CheckPanel({
  session,
  phase,
  onCollect,
  onSubmit,
  onClose,
  onNextCustomer,
  isSubmitting,
  isClosing,
  submitError,
  className,
}: CheckPanelProps) {
  const asideLayout =
    className ?? "w-full md:w-[380px] lg:w-[420px] shrink-0 border-l border-border";

  const isSettled = isPostPaymentPhase(phase);
  const openCheck = selectOpenCheck(session);
  const checks = listLiveChecks(session);
  const check = openCheck ?? checks[0] ?? null;
  const ambiguous = hasMultipleOpenChecks(session);

  const allocations = check?.allocations ?? [];
  const totalApplied = checks.reduce((sum, c) => sum + (c.total_applied_vnd ?? 0), 0);
  const changeGiven = latestPaymentChangeDue(check);
  const serviceNumber = session?.service_number;
  const heading = HEADINGS[phase];

  const showProgress = phase === "IN_PREPARATION" || phase === "READY_TO_CLOSE";
  const progress = preparationProgress(session);
  const percent = progress.total > 0 ? Math.round((progress.done * 100) / progress.total) : 0;

  return (
    <aside className={`flex flex-col bg-card overflow-hidden select-none ${asideLayout}`}>
      {/* Header */}
      <div className="flex items-center justify-between border-b border-border p-4 bg-muted/20 shrink-0">
        <div className="flex items-center gap-2.5">
          <div className="h-9 w-9 rounded-xl bg-primary/10 text-primary flex items-center justify-center">
            <Receipt className="h-5 w-5" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="text-sm font-bold text-foreground">
                {serviceNumber ? `Đơn mang đi #${serviceNumber}` : "Đơn mang đi"}
              </span>
              {heading && (
                <span className="rounded-md bg-emerald-100 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300 border border-emerald-200/60 px-2 py-0.5 text-2xs font-bold">
                  {heading.badge}
                </span>
              )}
            </div>
            {heading && <p className="text-2xs text-muted-foreground mt-0.5">{heading.caption}</p>}
          </div>
        </div>
      </div>

      {/* Committed items, read-only */}
      <div className="flex-1 overflow-y-auto p-4 space-y-2.5 bg-muted/10">
        {allocations.map((allocation) => (
          <div
            key={allocation.id}
            className="rounded-xl border border-border bg-card p-3 space-y-1"
          >
            <div className="flex items-start justify-between gap-2">
              <span className="text-sm font-bold text-foreground">{allocation.name}</span>
              <span className="font-mono text-sm font-bold text-foreground tabular-nums shrink-0">
                {formatVND(allocation.amount_vnd ?? 0)}
              </span>
            </div>
            <p className="text-2xs text-muted-foreground">
              {[
                `SL ${allocation.allocated_quantity ?? 0}`,
                allocation.size_name,
                ...(allocation.modifiers ?? []).map((m) => m.option_name),
              ]
                .filter(Boolean)
                .join(" · ")}
            </p>
            {allocation.preparation_note && (
              <p className="text-2xs italic text-muted-foreground">
                Ghi chú: {allocation.preparation_note}
              </p>
            )}
          </div>
        ))}
      </div>

      {ambiguous && (
        <div
          role="alert"
          className="mx-4 mb-2 flex items-start gap-2 rounded-xl bg-destructive/10 text-destructive px-3 py-2.5 text-xs font-semibold"
        >
          <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5" />
          <span>
            Phiên này có nhiều hóa đơn đang mở. Chức năng tách hóa đơn chưa hỗ trợ ở
            phiên bản này, vui lòng báo quản lý.
          </span>
        </div>
      )}

      {phase === "AWAITING_SUBMIT" && submitError && (
        <div
          role="alert"
          className="mx-4 mb-2 flex items-start gap-2 rounded-xl bg-amber-100 text-amber-900 dark:bg-amber-950/60 dark:text-amber-200 px-3 py-2.5 text-xs font-semibold"
        >
          <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
          <span>Chưa gửi được bếp. {submitError}</span>
        </div>
      )}

      {/* Kitchen progress */}
      {showProgress && (
        <div className="border-t border-border bg-card px-4 py-3 space-y-1.5 shrink-0">
          <div className="flex items-center justify-between text-xs">
            <span className="flex items-center gap-1.5 font-semibold text-foreground">
              <ChefHat className="h-3.5 w-3.5" />
              Pha chế
            </span>
            <span className="font-mono tabular-nums text-muted-foreground">
              {`Đã xong ${progress.done}/${progress.total} món`}
            </span>
          </div>
          <div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
            <div className="h-full rounded-full bg-primary transition-all" style={{ width: `${percent}%` }} />
          </div>
        </div>
      )}

      {/* Financial summary */}
      <div className="border-t border-border bg-card p-4 space-y-1.5 shrink-0 shadow-2xs">
        {isSettled ? (
          <>
            <div className="flex items-baseline justify-between text-xs text-muted-foreground">
              <span>Đã thu</span>
              <span className="font-mono tabular-nums font-semibold text-foreground">
                {formatVND(totalApplied)}
              </span>
            </div>
            <div className="flex items-baseline justify-between pt-1 border-t border-border/50">
              <span className="text-sm font-bold text-foreground">Tiền thối</span>
              <span className="font-mono text-2xl font-bold text-primary tabular-nums">
                {formatVND(changeGiven)}
              </span>
            </div>
          </>
        ) : (
          <div className="flex items-baseline justify-between pt-1">
            <span className="text-sm font-bold text-foreground">Còn phải thu</span>
            <span className="font-mono text-2xl font-bold text-primary tabular-nums">
              {formatVND(check?.balance_vnd ?? 0)}
            </span>
          </div>
        )}
      </div>

      <CheckPanelActions
        phase={phase}
        canCollect={!ambiguous && Boolean(openCheck)}
        isSubmitting={isSubmitting}
        isClosing={isClosing}
        onCollect={onCollect}
        onSubmit={onSubmit}
        onClose={onClose}
        onNextCustomer={onNextCustomer}
      />
    </aside>
  );
}
```

- [ ] **Step 5: Keep `pos-view.tsx` compiling**

Task 8 rewrites this call site. For now, pass these props to `<CheckPanel>`, with a temporary no-op for `onClose`:

```tsx
              onSubmit={checkout.submitOrder}
              onClose={() => {}}
              isSubmitting={checkout.submitStatus === "submitting"}
              isClosing={false}
              submitError={checkout.submitError}
```

- [ ] **Step 6: Run the tests and the type check**

Run: `cd web && bun test src/features/pos && bunx tsc -b`
Expected: PASS, with no type errors.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/pos/components/check-panel.tsx web/src/features/pos/components/check-panel-actions.tsx web/src/features/pos/components/check-panel.test.tsx web/src/features/pos/components/pos-view.tsx
git commit -m "feat(web): check panel for submit, preparation and closure"
```

---

## Task 7: Pending orders drawer

**Files:**
- Create: `web/src/features/pos/utils/pending-orders.ts`, `web/src/features/pos/utils/pending-orders.test.ts`
- Create: `web/src/features/pos/components/pending-order-row.tsx`
- Create: `web/src/features/pos/components/pending-orders-drawer.tsx`, `web/src/features/pos/components/pending-orders-drawer.test.tsx`

**Interfaces:**
- Consumes: `derivePosPhase`, `listLiveChecks`, `preparationProgress`, `PosPhase`, `PreparationProgress` (Task 2); `calculateDraftSubtotal` from `./pricing`.
- Produces:
  - `interface PendingOrder { sessionId: string; serviceNumber: string; createdAt: string; phase: PosPhase; progress: PreparationProgress; totalVnd: number; itemSummary: string }`
  - `PENDING_PHASE_LABELS: Record<PosPhase, string>`
  - `toPendingOrders(sessions?: SalesServiceSessionResponse[] | null): PendingOrder[]`
  - `countReadyToClose(orders: PendingOrder[]): number`
  - `summarizeItems(names: string[]): string`
  - `minutesSince(iso: string, nowMs: number): number`, `formatAge(minutes: number): string`
  - `<PendingOrdersButton count readyCount onClick />`
  - `<PendingOrdersDrawer isOpen orders isLoading errorMessage activeSessionId nowMs onSelect onClose onRetry />`

- [ ] **Step 1: Write the failing tests**

Create `web/src/features/pos/utils/pending-orders.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import {
  toPendingOrders,
  countReadyToClose,
  summarizeItems,
  minutesSince,
  formatAge,
} from "./pending-orders";
import type { SalesServiceSessionResponse } from "@/api/generated/models";

function s(overrides: Partial<SalesServiceSessionResponse>): SalesServiceSessionResponse {
  return {
    id: "s",
    service_number: "000",
    service_mode: "TAKEAWAY",
    state: "ACTIVE",
    checks: [],
    preparation_units: [],
    ...overrides,
  };
}

const paid = (submitted: boolean) => [
  {
    id: "c",
    state: "SETTLED",
    charge_vnd: 47_000,
    allocations: [{ id: "a", name: "Cà phê sữa đá", submitted }],
  },
];

describe("toPendingOrders", () => {
  it("keeps takeaway sessions only", () => {
    const orders = toPendingOrders([
      s({ id: "t", service_mode: "TAKEAWAY" }),
      s({ id: "d", service_mode: "DINE_IN" }),
    ]);
    expect(orders.map((o) => o.sessionId)).toEqual(["t"]);
  });

  it("puts ready-to-close first, then awaiting-submit, then the rest oldest first", () => {
    const orders = toPendingOrders([
      s({ id: "drafting-old", created_at: "2026-09-25T01:00:00Z", draft: { state: "EDITABLE", items: [] } }),
      s({
        id: "cooking",
        created_at: "2026-09-25T00:30:00Z",
        checks: paid(true),
        preparation_units: [{ state: "QUEUED" }],
      }),
      s({ id: "unsent", created_at: "2026-09-25T02:00:00Z", checks: paid(false) }),
      s({
        id: "ready",
        created_at: "2026-09-25T03:00:00Z",
        checks: paid(true),
        preparation_units: [{ state: "FULFILLED" }],
      }),
    ]);
    expect(orders.map((o) => o.sessionId)).toEqual(["ready", "unsent", "cooking", "drafting-old"]);
    expect(orders.map((o) => o.phase)).toEqual([
      "READY_TO_CLOSE",
      "AWAITING_SUBMIT",
      "IN_PREPARATION",
      "DRAFTING",
    ]);
  });

  it("totals a committed session from its Checks and summarizes its items", () => {
    const [order] = toPendingOrders([s({ id: "x", checks: paid(false) })]);
    expect(order.totalVnd).toBe(47_000);
    expect(order.itemSummary).toBe("Cà phê sữa đá");
  });

  it("tolerates a missing list", () => {
    expect(toPendingOrders(undefined)).toEqual([]);
  });

  it("counts the orders ready to close", () => {
    const orders = toPendingOrders([
      s({ id: "a", checks: paid(true), preparation_units: [{ state: "FULFILLED" }] }),
      s({ id: "b", checks: paid(false) }),
    ]);
    expect(countReadyToClose(orders)).toBe(1);
  });
});

describe("summarizeItems", () => {
  it("names up to two items, then counts the rest", () => {
    expect(summarizeItems(["A", "B", "C", "D"])).toBe("A, B +2 món");
    expect(summarizeItems(["A", "B"])).toBe("A, B");
    expect(summarizeItems([])).toBe("Chưa có món");
    expect(summarizeItems(["", ""])).toBe("Chưa có món");
  });
});

describe("age", () => {
  it("counts whole minutes and never goes negative", () => {
    const now = Date.parse("2026-09-25T01:10:30Z");
    expect(minutesSince("2026-09-25T01:00:00Z", now)).toBe(10);
    expect(minutesSince("2026-09-25T02:00:00Z", now)).toBe(0);
    expect(minutesSince("garbage", now)).toBe(0);
  });

  it("words the age", () => {
    expect(formatAge(0)).toBe("Vừa xong");
    expect(formatAge(7)).toBe("7 phút trước");
  });
});
```

Create `web/src/features/pos/components/pending-orders-drawer.test.tsx`:

```tsx
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { PendingOrdersDrawer, PendingOrdersButton } from "./pending-orders-drawer";
import type { PendingOrder } from "../utils/pending-orders";

const order = (overrides: Partial<PendingOrder>): PendingOrder => ({
  sessionId: "s1",
  serviceNumber: "012",
  createdAt: "2026-09-25T01:00:00Z",
  phase: "IN_PREPARATION",
  progress: { done: 1, total: 3 },
  totalVnd: 47_000,
  itemSummary: "Cà phê sữa đá",
  ...overrides,
});

const base = {
  isOpen: true,
  orders: [] as PendingOrder[],
  isLoading: false,
  errorMessage: null,
  activeSessionId: null,
  nowMs: Date.parse("2026-09-25T01:05:00Z"),
  onSelect: () => {},
  onClose: () => {},
  onRetry: () => {},
};

describe("PendingOrdersDrawer", () => {
  it("renders nothing while closed", () => {
    expect(renderToString(<PendingOrdersDrawer {...base} isOpen={false} />)).toBe("");
  });

  it("says so when nothing is waiting", () => {
    expect(renderToString(<PendingOrdersDrawer {...base} />)).toContain(
      "Không có đơn nào đang chờ",
    );
  });

  it("renders each order with status, progress, total and age", () => {
    const html = renderToString(
      <PendingOrdersDrawer
        {...base}
        orders={[order({}), order({ sessionId: "s2", serviceNumber: "013", phase: "AWAITING_SUBMIT" })]}
        activeSessionId="s2"
      />,
    );
    expect(html).toContain("#012");
    expect(html).toContain("Đang pha chế");
    expect(html).toContain("1/3 món xong");
    expect(html).toContain("47.000");
    expect(html).toContain("5 phút trước");
    expect(html).toContain("#013");
    expect(html).toContain("Chờ gửi bếp");
    expect(html).toContain('aria-current="true"');
  });

  it("shows the error with a retry", () => {
    const html = renderToString(
      <PendingOrdersDrawer {...base} errorMessage="Không kết nối được máy chủ." />,
    );
    expect(html).toContain("Không kết nối được máy chủ.");
    expect(html).toContain("Thử lại");
  });
});

describe("PendingOrdersButton", () => {
  it("shows the count and the ready badge", () => {
    const html = renderToString(<PendingOrdersButton count={3} readyCount={2} onClick={() => {}} />);
    expect(html).toContain("Đơn đang chờ (3)");
    expect(html).toContain("2 đơn sẵn sàng hoàn tất");
    expect(html).toContain("F4");
  });

  it("hides the ready badge at zero", () => {
    const html = renderToString(<PendingOrdersButton count={1} readyCount={0} onClick={() => {}} />);
    expect(html).not.toContain("sẵn sàng hoàn tất");
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && bun test src/features/pos/utils/pending-orders.test.ts src/features/pos/components/pending-orders-drawer.test.tsx`
Expected: FAIL, because the modules do not exist.

- [ ] **Step 3: Implement `utils/pending-orders.ts`**

```ts
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
```

- [ ] **Step 4: Implement `components/pending-order-row.tsx`**

```tsx
import type { PosPhase } from "../utils/phase";
import {
  PENDING_PHASE_LABELS,
  formatAge,
  minutesSince,
  type PendingOrder,
} from "../utils/pending-orders";
import { formatVND } from "@/lib/utils";

const CHIP_TONES: Record<PosPhase, string> = {
  READY_TO_CLOSE: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300",
  AWAITING_SUBMIT: "bg-amber-100 text-amber-800 dark:bg-amber-950/60 dark:text-amber-300",
  IN_PREPARATION: "bg-primary/10 text-primary",
  AWAITING_PAYMENT: "bg-muted text-foreground",
  DRAFTING: "bg-muted text-muted-foreground",
  NO_SESSION: "bg-muted text-muted-foreground",
};

export interface PendingOrderRowProps {
  order: PendingOrder;
  isActive: boolean;
  nowMs: number;
  onSelect: (sessionId: string) => void;
}

/** Tapping a row reopens that session; the right-hand panel then offers its action. */
export function PendingOrderRow({ order, isActive, nowMs, onSelect }: PendingOrderRowProps) {
  const showProgress = order.phase === "IN_PREPARATION" || order.phase === "READY_TO_CLOSE";
  const percent =
    order.progress.total > 0 ? Math.round((order.progress.done * 100) / order.progress.total) : 0;

  return (
    <button
      type="button"
      onClick={() => onSelect(order.sessionId)}
      aria-current={isActive ? "true" : undefined}
      className={`w-full min-h-[48px] rounded-xl border p-3 text-left space-y-1.5 select-none active:scale-[0.98] transition ${
        isActive ? "border-primary bg-primary/5" : "border-border bg-card hover:bg-muted"
      }`}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-bold text-foreground">{`#${order.serviceNumber}`}</span>
        <span className={`rounded-md px-2 py-0.5 text-2xs font-bold ${CHIP_TONES[order.phase]}`}>
          {PENDING_PHASE_LABELS[order.phase]}
        </span>
      </div>
      <div className="flex items-baseline justify-between gap-2 text-2xs text-muted-foreground">
        <span className="truncate">{order.itemSummary}</span>
        <span className="font-mono tabular-nums font-semibold text-foreground shrink-0">
          {formatVND(order.totalVnd)}
        </span>
      </div>
      {showProgress && (
        <div className="flex items-center gap-2">
          <div className="h-1.5 flex-1 rounded-full bg-muted overflow-hidden">
            <div className="h-full rounded-full bg-primary" style={{ width: `${percent}%` }} />
          </div>
          <span className="font-mono text-2xs tabular-nums text-muted-foreground">
            {`${order.progress.done}/${order.progress.total} món xong`}
          </span>
        </div>
      )}
      <p className="text-2xs text-muted-foreground">{formatAge(minutesSince(order.createdAt, nowMs))}</p>
    </button>
  );
}
```

- [ ] **Step 5: Implement `components/pending-orders-drawer.tsx`**

```tsx
import { X, ClipboardList, RefreshCw, AlertCircle } from "lucide-react";
import { useHotkeys } from "react-hotkeys-hook";
import type { PendingOrder } from "../utils/pending-orders";
import { PendingOrderRow } from "./pending-order-row";

export interface PendingOrdersButtonProps {
  count: number;
  readyCount: number;
  onClick: () => void;
}

export function PendingOrdersButton({ count, readyCount, onClick }: PendingOrdersButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="min-h-[48px] rounded-xl border border-border bg-card px-4 text-sm font-bold text-foreground hover:bg-muted flex items-center gap-2 select-none active:scale-[0.98] transition"
    >
      <ClipboardList className="h-4 w-4" />
      <span>{`Đơn đang chờ (${count})`}</span>
      {readyCount > 0 && (
        <span
          aria-label={`${readyCount} đơn sẵn sàng hoàn tất`}
          className="min-w-5 h-5 rounded-full bg-emerald-600 px-1.5 text-2xs font-bold text-white flex items-center justify-center tabular-nums"
        >
          {readyCount}
        </span>
      )}
      <span className="text-2xs font-normal text-muted-foreground">F4</span>
    </button>
  );
}

export interface PendingOrdersDrawerProps {
  isOpen: boolean;
  orders: PendingOrder[];
  isLoading: boolean;
  errorMessage: string | null;
  activeSessionId: string | null;
  /** When the list was last fetched; ages are measured from it so render stays pure. */
  nowMs: number;
  onSelect: (sessionId: string) => void;
  onClose: () => void;
  onRetry: () => void;
}

/**
 * Every active takeaway session. It has no action buttons of its own:
 * reopening a session hands it to the Check panel, so each action has one path.
 */
export function PendingOrdersDrawer({
  isOpen,
  orders,
  isLoading,
  errorMessage,
  activeSessionId,
  nowMs,
  onSelect,
  onClose,
  onRetry,
}: PendingOrdersDrawerProps) {
  useHotkeys(
    "escape",
    (event) => {
      event.preventDefault();
      onClose();
    },
    { enabled: isOpen, enableOnFormTags: true },
  );

  if (!isOpen) return null;

  return (
    <div
      className="fixed inset-0 z-40 flex justify-end bg-slate-900/30 animate-in fade-in duration-150"
      onClick={onClose}
    >
      <aside
        role="dialog"
        aria-modal="true"
        aria-labelledby="pending-orders-title"
        onClick={(event) => event.stopPropagation()}
        className="flex h-full w-full max-w-sm flex-col border-l border-border bg-card shadow-2xl animate-in slide-in-from-right duration-200"
      >
        <div className="flex items-center justify-between border-b border-border p-4 bg-muted/20 shrink-0">
          <h3 id="pending-orders-title" className="text-base font-bold text-foreground">
            {`Đơn đang chờ (${orders.length})`}
          </h3>
          <button
            type="button"
            onClick={onClose}
            aria-label="Đóng"
            className="h-12 w-12 min-h-[48px] min-w-[48px] rounded-xl text-muted-foreground hover:text-foreground hover:bg-muted flex items-center justify-center select-none active:scale-[0.98] transition"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto p-3 space-y-2">
          {isLoading ? (
            [0, 1, 2].map((key) => (
              <div key={key} className="h-20 rounded-xl bg-muted animate-pulse" />
            ))
          ) : errorMessage ? (
            <div className="space-y-3">
              <div
                role="alert"
                className="flex items-start gap-2 rounded-xl bg-destructive/10 text-destructive px-3 py-2.5 text-xs font-semibold"
              >
                <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
                <span>{errorMessage}</span>
              </div>
              <button
                type="button"
                onClick={onRetry}
                className="min-h-[48px] w-full rounded-xl border border-border bg-card text-sm font-bold text-foreground hover:bg-muted flex items-center justify-center gap-2 select-none active:scale-[0.98] transition"
              >
                <RefreshCw className="h-4 w-4" />
                Thử lại
              </button>
            </div>
          ) : orders.length === 0 ? (
            <p className="py-12 text-center text-sm text-muted-foreground">
              Không có đơn nào đang chờ
            </p>
          ) : (
            orders.map((order) => (
              <PendingOrderRow
                key={order.sessionId}
                order={order}
                isActive={order.sessionId === activeSessionId}
                nowMs={nowMs}
                onSelect={onSelect}
              />
            ))
          )}
        </div>
      </aside>
    </div>
  );
}
```

- [ ] **Step 6: Run the tests and the type check**

Run: `cd web && bun test src/features/pos && bunx tsc -b`
Expected: PASS, with no type errors.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/pos/utils/pending-orders.ts web/src/features/pos/utils/pending-orders.test.ts web/src/features/pos/components/pending-order-row.tsx web/src/features/pos/components/pending-orders-drawer.tsx web/src/features/pos/components/pending-orders-drawer.test.tsx
git commit -m "feat(web): pending orders drawer for active takeaway sessions"
```

---

## Task 8: Hotkeys and POS view wiring

**Files:**
- Create: `web/src/features/pos/hooks/use-pos-hotkeys.ts`, `web/src/features/pos/hooks/use-pos-hotkeys.test.ts`
- Create: `web/src/features/pos/components/pos-menu-status.tsx`
- Create: `web/src/features/pos/components/pos-error-toast.tsx`
- Modify: `web/src/features/pos/components/pos-view.tsx`
- Modify: `web/src/features/pos/components/pos-view.test.tsx`

**Interfaces:**
- Consumes: everything from Tasks 3 through 7.
- Produces:
  - `type F9Action = "none" | "checkout" | "submit" | "next-customer" | "close"`
  - `resolveF9Action(phase: PosPhase, blocked: boolean): F9Action`
  - `usePosHotkeys(options: PosHotkeysOptions): void`
  - `<PosMenuLoading />`, `<PosMenuError message onRetry />`, `<PosErrorToast message onDismiss />`

- [ ] **Step 1: Write the failing test**

Create `web/src/features/pos/hooks/use-pos-hotkeys.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { resolveF9Action, usePosHotkeys } from "./use-pos-hotkeys";

describe("resolveF9Action", () => {
  it("follows the primary action of each phase", () => {
    expect(resolveF9Action("NO_SESSION", false)).toBe("checkout");
    expect(resolveF9Action("DRAFTING", false)).toBe("checkout");
    expect(resolveF9Action("AWAITING_PAYMENT", false)).toBe("checkout");
    expect(resolveF9Action("AWAITING_SUBMIT", false)).toBe("submit");
    expect(resolveF9Action("IN_PREPARATION", false)).toBe("next-customer");
    expect(resolveF9Action("READY_TO_CLOSE", false)).toBe("close");
  });

  it("does nothing while a dialog, the picker, or the drawer is open", () => {
    expect(resolveF9Action("READY_TO_CLOSE", true)).toBe("none");
    expect(resolveF9Action("DRAFTING", true)).toBe("none");
  });
});

describe("usePosHotkeys", () => {
  it("is exported", () => {
    expect(typeof usePosHotkeys).toBe("function");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && bun test src/features/pos/hooks/use-pos-hotkeys.test.ts`
Expected: FAIL, because the module does not exist.

- [ ] **Step 3: Implement `hooks/use-pos-hotkeys.ts`**

```ts
import { useHotkeys } from "react-hotkeys-hook";
import type { PosPhase } from "../utils/phase";

export type F9Action = "none" | "checkout" | "submit" | "next-customer" | "close";

/**
 * F9 always fires the panel's primary action; the labels in
 * components/check-panel-actions.tsx must match this table.
 */
export function resolveF9Action(phase: PosPhase, blocked: boolean): F9Action {
  if (blocked) return "none";
  switch (phase) {
    case "AWAITING_SUBMIT":
      return "submit";
    case "IN_PREPARATION":
      return "next-customer";
    case "READY_TO_CLOSE":
      return "close";
    default:
      return "checkout";
  }
}

export interface PosHotkeysOptions {
  phase: PosPhase;
  /** A dialog or the item picker is open, so the terminal keys belong to it. */
  blocked: boolean;
  isDrawerOpen: boolean;
  onCheckout: () => void;
  onSubmit: () => Promise<void>;
  onNextCustomer: () => void;
  onClose: () => Promise<void>;
  onToggleDrawer: () => void;
}

export function usePosHotkeys({
  phase,
  blocked,
  isDrawerOpen,
  onCheckout,
  onSubmit,
  onNextCustomer,
  onClose,
  onToggleDrawer,
}: PosHotkeysOptions): void {
  useHotkeys(
    "f9",
    (event) => {
      event.preventDefault();
      switch (resolveF9Action(phase, blocked || isDrawerOpen)) {
        case "checkout":
          onCheckout();
          break;
        case "submit":
          void onSubmit();
          break;
        case "next-customer":
          onNextCustomer();
          break;
        case "close":
          void onClose();
          break;
        default:
          break;
      }
    },
    { enableOnFormTags: true },
  );

  useHotkeys(
    "f4",
    (event) => {
      event.preventDefault();
      if (!blocked) onToggleDrawer();
    },
    { enableOnFormTags: true },
  );
}
```

- [ ] **Step 4: Extract `pos-menu-status.tsx` and `pos-error-toast.tsx`**

These move existing `pos-view.tsx` markup verbatim, so the view can take on the drawer without growing.

`web/src/features/pos/components/pos-menu-status.tsx`:

```tsx
import { Clock, AlertCircle, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";

export function PosMenuLoading() {
  return (
    <div className="flex flex-col items-center justify-center p-12 min-h-[60vh] gap-4">
      <div className="w-12 h-12 rounded-2xl bg-primary/10 text-primary flex items-center justify-center animate-pulse">
        <Clock className="w-6 h-6 animate-spin" />
      </div>
      <p className="text-sm text-muted-foreground font-medium">Đang tải thực đơn bán hàng...</p>
    </div>
  );
}

export function PosMenuError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center p-8 max-w-md mx-auto min-h-[60vh] text-center gap-4">
      <div className="w-12 h-12 rounded-2xl bg-destructive/10 text-destructive flex items-center justify-center">
        <AlertCircle className="w-6 h-6" />
      </div>
      <div className="space-y-1">
        <h3 className="text-base font-bold text-foreground">Không thể tải thực đơn</h3>
        <p className="text-xs text-muted-foreground">{message}</p>
      </div>
      <Button
        type="button"
        variant="outline"
        onClick={onRetry}
        className="rounded-xl h-10 min-h-[48px] px-6"
      >
        <RefreshCw className="w-4 h-4 mr-2" />
        Thử lại
      </Button>
    </div>
  );
}
```

`web/src/features/pos/components/pos-error-toast.tsx`:

```tsx
import { AlertCircle } from "lucide-react";

export function PosErrorToast({ message, onDismiss }: { message: string | null; onDismiss: () => void }) {
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

- [ ] **Step 5: Rewire `pos-view.tsx`**

Apply these edits. The item add, edit, quantity, and remove handlers stay as they are.

1. Replace the imports, from line 1 through the `messageForError` import, with:

```tsx
import * as React from "react";
import { useCurrentShift } from "@/features/shift/api/use-shift";
import {
  useSellableMenu,
  useActiveSessions,
  useAddDraftItem,
  useUpdateDraftItemQuantity,
  useUpdateDraftItemSize,
  useUpdateDraftItemModifiers,
  useUpdateDraftItemPreparationNote,
  useRemoveDraftItem,
} from "../api/use-pos";
import { usePosSession } from "../api/use-pos-session";
import { useCheckoutFlow } from "../api/use-checkout";
import { useCloseFlow } from "../api/use-close-session";
import { usePosHotkeys } from "../hooks/use-pos-hotkeys";
import { MenuGrid } from "./menu-grid";
import { DraftPanel } from "./draft-panel";
import { CheckPanel } from "./check-panel";
import { PaymentDialog } from "./payment-dialog";
import { CompletedSaleDialog } from "./completed-sale-dialog";
import { PendingOrdersDrawer, PendingOrdersButton } from "./pending-orders-drawer";
import { PosMenuLoading, PosMenuError } from "./pos-menu-status";
import { PosErrorToast } from "./pos-error-toast";
import { ItemPickerDialog, type ItemPickerConfig } from "./item-picker-dialog";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@/components/ui/resizable";
import { matchesDraftItemConfig, diffDraftItemEdits } from "../utils/selection";
import { derivePosPhase, selectOpenCheck, isPostPaymentPhase } from "../utils/phase";
import { calculateDraftSubtotal } from "../utils/pricing";
import { toPendingOrders, countReadyToClose } from "../utils/pending-orders";
import type {
  CatalogSellableItemResponse,
  SalesDraftItemResponse,
} from "@/api/generated/models";
import { messageForError } from "@/lib/error-messages";
```

2. Replace the `usePosSession` line with:

```tsx
  const { activeSessionId, session, ensureSessionId, clearSession, switchSession } =
    usePosSession();

  const [isDrawerOpen, setIsDrawerOpen] = React.useState(false);
  const activeSessions = useActiveSessions(isDrawerOpen);
  const pendingOrders = toPendingOrders(activeSessions.data);
```

3. After the `useCheckoutFlow({...})` call, add:

```tsx
  const closeFlow = useCloseFlow({
    activeSessionId,
    session,
    clearSession,
    onError: setErrorMessage,
  });

  const handleSelectPendingOrder = (sessionId: string) => {
    setIsDrawerOpen(false);
    setErrorMessage(null);
    switchSession(sessionId);
  };
```

4. Replace the whole `useHotkeys("f9", ...)` block with:

```tsx
  usePosHotkeys({
    phase,
    blocked: checkout.isPaymentOpen || isPickerOpen || closeFlow.completedSale !== null,
    isDrawerOpen,
    onCheckout: checkout.openPaymentDialog,
    onSubmit: checkout.submitOrder,
    onNextCustomer: checkout.nextCustomer,
    onClose: closeFlow.closeSession,
    onToggleDrawer: () => setIsDrawerOpen((open) => !open),
  });
```

5. Replace the loading and menu-error early returns with:

```tsx
  if (isShiftLoading || isMenuLoading) return <PosMenuLoading />;

  if (isMenuError) {
    return <PosMenuError message={messageForError(menuError)} onRetry={() => refetchMenu()} />;
  }
```

6. In Zone 1, put the entry button above `<MenuGrid>`:

```tsx
          <div className="flex items-center justify-end border-b border-border px-3 py-2 shrink-0">
            <PendingOrdersButton
              count={pendingOrders.length}
              readyCount={countReadyToClose(pendingOrders)}
              onClick={() => setIsDrawerOpen(true)}
            />
          </div>
```

7. Replace the `<CheckPanel>` element's props with:

```tsx
            <CheckPanel
              session={session}
              phase={phase}
              onCollect={checkout.openPaymentDialog}
              onSubmit={() => void checkout.submitOrder()}
              onClose={() => void closeFlow.closeSession()}
              onNextCustomer={checkout.nextCustomer}
              isSubmitting={checkout.submitStatus === "submitting"}
              isClosing={closeFlow.isClosing}
              submitError={checkout.submitError}
              className="w-full h-full flex-1"
            />
```

8. After `<PaymentDialog ... />`, replace the error toast block with:

```tsx
      <CompletedSaleDialog
        sale={closeFlow.completedSale}
        onDone={closeFlow.dismissCompletedSale}
      />

      <PendingOrdersDrawer
        isOpen={isDrawerOpen}
        orders={pendingOrders}
        isLoading={activeSessions.isLoading}
        errorMessage={activeSessions.isError ? messageForError(activeSessions.error) : null}
        activeSessionId={activeSessionId}
        nowMs={activeSessions.dataUpdatedAt}
        onSelect={handleSelectPendingOrder}
        onClose={() => setIsDrawerOpen(false)}
        onRetry={() => void activeSessions.refetch()}
      />

      <PosErrorToast message={errorMessage} onDismiss={() => setErrorMessage(null)} />
```

9. Delete the now-unused imports (`Clock`, `AlertCircle`, `RefreshCw`, `useHotkeys`, `Button`).

- [ ] **Step 6: Update the `pos-view.test.tsx` mocks**

Add to the `mock.module("../api/use-pos", ...)` object:

```tsx
  useActiveSessions: () => ({
    data: [],
    isLoading: false,
    isError: false,
    error: null,
    dataUpdatedAt: 0,
    refetch: async () => ({}),
  }),
  useCompletedSale: () => ({ data: undefined, isError: false }),
```

Add a new mock before `import { PosView } from "./pos-view";`:

```tsx
mock.module("../api/use-close-session", () => ({
  useCloseFlow: () => ({
    isClosing: false,
    completedSale: null,
    closeSession: async () => {},
    dismissCompletedSale: () => {},
  }),
}));
```

- [ ] **Step 7: Run the full suite, lint, the type check, and the line count**

Run: `cd web && bun test && bun run lint && bunx tsc -b && wc -l src/features/pos/components/pos-view.tsx`
Expected: every test passes, lint and tsc are clean, and `pos-view.tsx` is at most 390 lines.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/pos/hooks web/src/features/pos/components/pos-menu-status.tsx web/src/features/pos/components/pos-error-toast.tsx web/src/features/pos/components/pos-view.tsx web/src/features/pos/components/pos-view.test.tsx
git commit -m "feat(web): wire pending orders, closure and phase hotkeys into the POS"
```

---

## Task 9: UAT helper script

Until Slice 6 ships the Preparation Queue, nothing in the UI moves a Preparation Unit, so UAT cannot reach `READY_TO_CLOSE` (spec section 2). This script is scaffolding. It is deleted when Slice 6 lands.

**Files:**
- Create: `scripts/uat-advance-units.ts`

**Interfaces:**
- Consumes: `POST /auth/sign-in`, `GET /sales/service-sessions`, `POST /preparation/units/advance-many` (`{ request_id, preparation_unit_ids, target_state }`, 1 to 50 ids, target one of `IN_PREPARATION`, `READY`, `FULFILLED`).

- [ ] **Step 1: Write the script**

```ts
/**
 * UAT scaffolding for Web Slice 5. Advances every unfinished Preparation Unit
 * of one active Service Session to FULFILLED, standing in for the Preparation
 * Queue that Slice 6 delivers. Delete this file when Slice 6 ships.
 *
 * Usage: bun run scripts/uat-advance-units.ts <service_number | session_id>
 * Env:   POS_API_BASE (default http://localhost:8080/api/v1)
 *        POS_UAT_LOGIN (default QL01), POS_UAT_PIN (default 1234)
 */
const BASE = process.env.POS_API_BASE ?? "http://localhost:8080/api/v1";
const LOGIN = process.env.POS_UAT_LOGIN ?? "QL01";
const PIN = process.env.POS_UAT_PIN ?? "1234";

// Advance is one step at a time: QUEUED -> IN_PREPARATION -> READY -> FULFILLED.
const STEPS = ["IN_PREPARATION", "READY", "FULFILLED"] as const;
const TERMINAL = new Set(["FULFILLED", "CANCELLED", "WASTED"]);
const BATCH = 50;

interface Unit {
  id?: string;
  state?: string;
}

interface Session {
  id?: string;
  service_number?: string;
  preparation_units?: Unit[];
}

async function api<T>(path: string, init: { method?: string; body?: unknown; token?: string }) {
  const res = await fetch(`${BASE}${path}`, {
    method: init.method ?? "POST",
    headers: {
      "Content-Type": "application/json",
      ...(init.token ? { Authorization: `Bearer ${init.token}` } : {}),
    },
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
  });
  const payload = (await res.json().catch(() => null)) as { data?: T } | null;
  if (!res.ok) throw new Error(`${path} -> ${res.status} ${JSON.stringify(payload)}`);
  return payload?.data as T;
}

async function main(): Promise<void> {
  const target = process.argv[2];
  if (!target) {
    console.error("Usage: bun run scripts/uat-advance-units.ts <service_number | session_id>");
    process.exit(1);
  }

  const auth = await api<{ token?: string }>("/auth/sign-in", {
    body: { login_code: LOGIN, pin: PIN },
  });
  const token = auth?.token ?? "";

  const sessions = await api<Session[]>("/sales/service-sessions", { method: "GET", token });
  const session = (sessions ?? []).find(
    (s) => s.id === target || s.service_number === target,
  );
  if (!session) {
    console.error(`No active session matches "${target}".`);
    process.exit(1);
  }

  const unitIds = (session.preparation_units ?? [])
    .filter((unit) => unit.id && !TERMINAL.has(unit.state ?? ""))
    .map((unit) => unit.id as string);
  if (unitIds.length === 0) {
    console.log(`Session #${session.service_number}: no unfinished units.`);
    return;
  }

  for (const step of STEPS) {
    for (let i = 0; i < unitIds.length; i += BATCH) {
      const batch = unitIds.slice(i, i + BATCH);
      // Units already past this step fail individually; the batch still succeeds.
      await api("/preparation/units/advance-many", {
        token,
        body: { request_id: crypto.randomUUID(), preparation_unit_ids: batch, target_state: step },
      });
    }
    console.log(`Session #${session.service_number}: advanced ${unitIds.length} unit(s) to ${step}.`);
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
```

- [ ] **Step 2: Type-check the script**

Run: `bunx tsc --noEmit --target es2022 --module esnext --moduleResolution bundler --types bun scripts/uat-advance-units.ts`
Expected: no errors. If `bun-types` is not installed at the root, run `bun build scripts/uat-advance-units.ts --outdir /tmp/uat-check` instead and expect a clean build.

- [ ] **Step 3: Smoke-run it against a local server**

With the Go API running and a submitted takeaway order open, run `bun run scripts/uat-advance-units.ts <service_number>`.
Expected: three lines ending in `to FULFILLED`. The POS then shows the order as "Sẵn sàng hoàn tất" within 5 s.

- [ ] **Step 4: Commit**

```bash
git add scripts/uat-advance-units.ts
git commit -m "chore: add UAT script that advances preparation units until Slice 6"
```

---

## Task 10: UAT gate

**Files:**
- Create: `.superpowers/sdd/2026-09-25-web-slice-5-pos-submit-close/uat-handover.md`

- [ ] **Step 1: Run the full verification**

```bash
cd web && bun test && bun run lint && bunx tsc -b && bun run build
```

Expected: every command succeeds. Paste the tail of each output into the handover.

- [ ] **Step 2: Check the spec's section 11.1 checklist by hand**

```bash
cd web && grep -rn '"SETTLED"' src/features/pos --include=*.ts --include=*.tsx | grep -v 'state: "SETTLED"'
wc -l src/features/pos/components/pos-view.tsx
```

Expected: the grep finds no `SETTLED` phase references; only Check-state fixtures remain. The line count is at most 390.

- [ ] **Step 3: Write the handover**

Create `.superpowers/sdd/2026-09-25-web-slice-5-pos-submit-close/uat-handover.md`, and copy these UAT scripts into it verbatim:

```markdown
# Web Slice 5 UAT handover

## Preconditions
- Go API running; `bun run scripts/dev-seed.ts` has seeded the catalog.
- A Sales Shift is open.
- Checks run in the web app at the cashier workspace.

## Happy path
1. Add two items, press F9, pay cash. Expect the result screen to show the change and "Đã gửi bếp".
2. Press "Xong", then "Khách tiếp theo" (F9). Expect an empty draft.
3. Press F4. Expect the order listed as "Đang pha chế 0/y món xong".
4. Run `bun run scripts/uat-advance-units.ts <service_number>`.
5. Within 5 s, expect the order to move to the top as "Sẵn sàng hoàn tất", with a green count on the button.
6. Tap it, then press F9 ("Hoàn tất"). Expect the Completed Sale dialog with the correct items, totals, cash tendered, change, and "n món đã giao".
7. Press Enter. Expect the POS to be empty, and the order gone from the drawer.

## Unhappy paths
1. Stop the network after pressing confirm in the payment dialog, once payment is recorded but before submit. Expect "Đã thu tiền nhưng chưa gửi được bếp". Restore the network and press "Gửi bếp (F9)". Expect exactly one Order on the session.
2. Open a session stranded by Slice 4 (listed as "Chờ gửi bếp") from the drawer, then press "Gửi bếp". Expect it to move to "Đang pha chế".
3. In a second tab, open the same order while it is `IN_PREPARATION`. Advance only some units, then force "Hoàn tất" from the first tab after its poll made it ready. Expect the Vietnamese refusal, and the phase to return to `IN_PREPARATION`.
4. Close the same session from two tabs. Expect the second tab to show the same Completed Sale, with no error.
5. Reload during `IN_PREPARATION`. Expect the progress exactly as before.
6. Leave a draft half-built, switch to another order from the drawer, then switch back. Expect the draft unchanged.

## Verification output
(paste Step 1 output here)
```

- [ ] **Step 4: Commit and stop at the gate**

```bash
git add .superpowers/sdd/2026-09-25-web-slice-5-pos-submit-close/uat-handover.md
git commit -m "docs: UAT handover for web slice 5"
```

Report to the user that the slice is ready for UAT. Do not open the pull request or declare the slice done until the user confirms that UAT passed.
