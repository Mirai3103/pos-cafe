# Web Slice 4: POS-b Commit, Check & Cash Payment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Cashier Terminal take money — commit the Order Draft into an immutable Check and record a cash payment, showing the cashier the change to give back.

**Architecture:** The POS phase is *derived* from `ServiceSessionResponse` by a pure function rather than tracked in client state, because both endpoints return the complete projection and `LoadServiceSession` nulls `draft` once it is committed. Every mutation writes its response straight into the react-query session cache, so the phase advances without a refetch, and page reload or a failed second request recovers by re-deriving rather than by dedicated recovery code.

**Tech Stack:** React 19, TanStack Query v5, Tailwind CSS v4, Lucide icons, react-hotkeys-hook, Web Audio feedback (`playTapChirp` / `playSuccessChirp` / `playErrorBuzz`), `bun test`.

**Spec:** [`docs/superpowers/specs/2026-09-24-web-slice-4-pos-checkout-design.md`](../specs/2026-09-24-web-slice-4-pos-checkout-design.md)

---

## Global Constraints

- **The server owns every number that represents money.** `applied_amount_vnd` comes from the commit response's `balance_vnd`; the change shown to the cashier comes from `payments[].change_due_vnd`. `pricing.ts` stays display-only.
- **No client-side state machine.** `derivePosPhase` is the only source of POS phase. A component that stores "we are now awaiting payment" in `useState` is a plan violation.
- **Single-Intent Idempotency:** every mutation carries a `request_id` from `newRequestId()`, held in a `useRef` and retained across retries within one dialog lifetime.
- **No end-to-end or browser integration tests.** Unit tests only, via `bun test`: pure logic plus `renderToString` output assertions.
- **Strict Touch Target Dimensions:** every touchable element keeps `min-h-[48px]` (and `min-w-[48px]` for icon buttons).
- **Tactile Sound & Press Feedback:** button presses use `active:scale-[0.98]` and a Web Audio chirp.
- **Whole VND Currency Only:** JetBrains Mono (`font-mono`), dot thousand-separators via `formatVND`. No decimals.
- **Banned Elements:** no emojis, no em-dashes, no neon glows, no pure black (`#000000` -> `#0f172a`).
- **All generated model fields are optional.** Orval emits `?` on every property of `SalesCheckResponse`, `SalesPaymentResponse`, and friends. Every read must tolerate `undefined`.
- **The slice ends at a UAT gate** (Task 10). Completion is never self-certified.

---

## File Structure

**Create**

| File | Responsibility |
| :--- | :--- |
| `web/src/features/pos/utils/payment.ts` | Change due, tender sufficiency, quick-tender suggestions, latest-payment lookup |
| `web/src/features/pos/utils/payment.test.ts` | Tests for the above |
| `web/src/features/pos/utils/phase.ts` | `derivePosPhase` and Check selection helpers |
| `web/src/features/pos/utils/phase.test.ts` | Tests for the above |
| `web/src/features/pos/api/use-checkout.ts` | `useCommitDraft`, `usePayCash` |
| `web/src/features/pos/api/use-checkout.test.ts` | Export smoke test |
| `web/src/features/pos/api/use-pos-session.ts` | Service Session id lifecycle extracted from `pos-view.tsx` |
| `web/src/features/pos/components/payment-dialog.tsx` | Checkout dialog (presentational) |
| `web/src/features/pos/components/payment-dialog.test.tsx` | Render assertions |
| `web/src/features/pos/components/check-panel.tsx` | Committed and settled bill faces |
| `web/src/features/pos/components/check-panel.test.tsx` | Render assertions |
| `.superpowers/sdd/2026-09-24-web-slice-4-pos-checkout/uat-handover.md` | UAT gate script |

**Modify**

| File | Change |
| :--- | :--- |
| `web/src/lib/utils.ts` | Gains `VND_DENOMINATIONS` |
| `web/src/features/shift/utils/denomination.ts` | Re-exports `VND_DENOMINATIONS` from `lib/utils.ts` |
| `web/src/lib/error-messages.ts` | Commit and payment error codes |
| `web/src/lib/error-messages.test.ts` | Coverage for the new codes |
| `web/src/features/pos/components/draft-panel.tsx` | Live "Thanh toán (F9)" button; VietQR tab stays disabled |
| `web/src/features/pos/components/draft-panel.test.tsx` | Assertions for the live button |
| `web/src/features/pos/components/pos-view.tsx` | Session state extracted out; phase switch; checkout orchestration |

---

## Task 1: Shared VND denominations

The quick-tender row needs the denomination ladder that currently lives inside the shift feature. `formatVND` already sits in `lib/utils.ts`, so the constant joins it there rather than being imported across feature boundaries.

**Files:**
- Modify: `web/src/lib/utils.ts`
- Modify: `web/src/features/shift/utils/denomination.ts:1-3`
- Test: `web/src/lib/utils.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `VND_DENOMINATIONS: readonly number[]` exported from `@/lib/utils`, descending order, `[500000, 200000, 100000, 50000, 20000, 10000, 5000, 2000, 1000]`.

- [ ] **Step 1: Write the failing test**

Append to `web/src/lib/utils.test.ts`:

```ts
import { VND_DENOMINATIONS } from "./utils";

describe("VND_DENOMINATIONS", () => {
  it("lists every Vietnamese note in descending order", () => {
    expect([...VND_DENOMINATIONS]).toEqual([
      500_000, 200_000, 100_000, 50_000, 20_000, 10_000, 5_000, 2_000, 1_000,
    ]);
  });
});
```

Add `VND_DENOMINATIONS` to the existing `import { formatVND } from "./utils";` line instead of writing a second import statement.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/lib/utils.test.ts`
Expected: FAIL — `VND_DENOMINATIONS` is not exported.

- [ ] **Step 3: Move the constant**

Append to `web/src/lib/utils.ts`:

```ts
/**
 * Every Vietnamese banknote in circulation, largest first.
 *
 * Shared by the Shift denomination counter and the POS quick-tender row, so
 * it lives beside formatVND rather than inside either feature.
 */
export const VND_DENOMINATIONS = [
  500_000, 200_000, 100_000, 50_000, 20_000, 10_000, 5_000, 2_000, 1_000,
] as const;
```

Replace the first three lines of `web/src/features/shift/utils/denomination.ts` with a re-export so every existing import in the shift feature keeps working:

```ts
import { VND_DENOMINATIONS } from "@/lib/utils";

export { VND_DENOMINATIONS };
```

Leave `DenominationCounts`, `calculateDenominationTotal`, and `formatDenomination` in that file untouched.

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/lib/utils.test.ts src/features/shift/utils/denomination.test.ts`
Expected: PASS, both files.

- [ ] **Step 5: Typecheck and commit**

```bash
cd web && bun run lint && tsc -b
git add web/src/lib/utils.ts web/src/lib/utils.test.ts web/src/features/shift/utils/denomination.ts
git commit -m "refactor(web): share VND denominations from lib/utils"
```

---

## Task 2: Payment arithmetic

**Files:**
- Create: `web/src/features/pos/utils/payment.ts`
- Test: `web/src/features/pos/utils/payment.test.ts`

**Interfaces:**
- Consumes: `VND_DENOMINATIONS` from `@/lib/utils` (Task 1).
- Produces:
  - `changeDue(tenderedVnd: number, totalVnd: number): number`
  - `isTenderSufficient(tenderedVnd: number, totalVnd: number): boolean`
  - `suggestTenders(totalVnd: number): number[]`
  - `latestPaymentChangeDue(check?: PayableCheck | null): number`
  - `interface PayableCheck { payments?: Array<{ change_due_vnd?: number; received_at?: string }> }`

- [ ] **Step 1: Write the failing test**

Create `web/src/features/pos/utils/payment.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import {
  changeDue,
  isTenderSufficient,
  suggestTenders,
  latestPaymentChangeDue,
} from "./payment";

describe("changeDue", () => {
  it("returns the difference when the customer overpays", () => {
    expect(changeDue(50_000, 47_000)).toBe(3_000);
  });

  it("returns zero on exact tender", () => {
    expect(changeDue(47_000, 47_000)).toBe(0);
  });

  it("clamps to zero when the tender is short", () => {
    expect(changeDue(40_000, 47_000)).toBe(0);
  });
});

describe("isTenderSufficient", () => {
  it("accepts a tender above the total", () => {
    expect(isTenderSufficient(50_000, 47_000)).toBe(true);
  });

  it("accepts an exact tender", () => {
    expect(isTenderSufficient(47_000, 47_000)).toBe(true);
  });

  it("rejects a short tender", () => {
    expect(isTenderSufficient(46_999, 47_000)).toBe(false);
  });

  it("rejects a total of zero, which is never payable", () => {
    expect(isTenderSufficient(0, 0)).toBe(false);
  });
});

describe("suggestTenders", () => {
  it("offers the exact total then the next notes up", () => {
    expect(suggestTenders(47_000)).toEqual([47_000, 50_000, 100_000, 200_000]);
  });

  it("does not repeat the total when it lands on a note", () => {
    expect(suggestTenders(200_000)).toEqual([200_000, 500_000]);
  });

  it("offers only the total once it exceeds the largest note", () => {
    expect(suggestTenders(600_000)).toEqual([600_000]);
  });

  it("returns nothing for a non-positive total", () => {
    expect(suggestTenders(0)).toEqual([]);
  });
});

describe("latestPaymentChangeDue", () => {
  it("reads the change of the most recent payment", () => {
    expect(
      latestPaymentChangeDue({
        payments: [
          { change_due_vnd: 3_000, received_at: "2026-09-24T02:00:00Z" },
          { change_due_vnd: 7_000, received_at: "2026-09-24T03:00:00Z" },
        ],
      }),
    ).toBe(7_000);
  });

  it("treats a missing change field as zero", () => {
    expect(
      latestPaymentChangeDue({ payments: [{ received_at: "2026-09-24T02:00:00Z" }] }),
    ).toBe(0);
  });

  it("returns zero when there are no payments", () => {
    expect(latestPaymentChangeDue({ payments: [] })).toBe(0);
    expect(latestPaymentChangeDue(null)).toBe(0);
    expect(latestPaymentChangeDue(undefined)).toBe(0);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/pos/utils/payment.test.ts`
Expected: FAIL — module `./payment` not found.

- [ ] **Step 3: Write the implementation**

Create `web/src/features/pos/utils/payment.ts`:

```ts
import { VND_DENOMINATIONS } from "@/lib/utils";

/** The slice of a Check this module reads. Orval marks every field optional. */
export interface PayableCheck {
  payments?: Array<{ change_due_vnd?: number; received_at?: string }>;
}

const MAX_TENDER_SUGGESTIONS = 4;

/**
 * Change owed back to the customer, previewed live while they type.
 *
 * The figure shown after a successful payment comes from the server's
 * change_due_vnd instead; this one only drives the keypad preview.
 */
export function changeDue(tenderedVnd: number, totalVnd: number): number {
  return Math.max(0, tenderedVnd - totalVnd);
}

/** Whether the cash on the counter covers the Check. */
export function isTenderSufficient(tenderedVnd: number, totalVnd: number): boolean {
  if (totalVnd <= 0) return false;
  return tenderedVnd >= totalVnd;
}

/**
 * Quick-tender buttons: the exact total first, then the notes above it.
 *
 * A cashier handed a 100.000 note for a 47.000 order taps one button rather
 * than typing six digits.
 */
export function suggestTenders(totalVnd: number): number[] {
  if (totalVnd <= 0) return [];

  const ascending = [...VND_DENOMINATIONS].sort((a, b) => a - b);
  const suggestions = [totalVnd];

  for (const note of ascending) {
    if (suggestions.length >= MAX_TENDER_SUGGESTIONS) break;
    if (note > totalVnd) suggestions.push(note);
  }

  return suggestions;
}

/**
 * The change the server recorded for the Check's most recent Payment.
 *
 * Payments are appended, so the newest received_at wins. A Check with no
 * payment, or a Manual QR payment carrying no cash fields, reports zero.
 */
export function latestPaymentChangeDue(check?: PayableCheck | null): number {
  const payments = check?.payments ?? [];
  if (payments.length === 0) return 0;

  const newest = payments.reduce((latest, candidate) => {
    const latestAt = latest.received_at ?? "";
    const candidateAt = candidate.received_at ?? "";
    return candidateAt > latestAt ? candidate : latest;
  });

  return newest.change_due_vnd ?? 0;
}
```

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/features/pos/utils/payment.test.ts`
Expected: PASS, 14 assertions.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/pos/utils/payment.ts web/src/features/pos/utils/payment.test.ts
git commit -m "feat(web): add cash payment arithmetic for pos checkout"
```

---

## Task 3: POS phase derivation

The heart of the slice. Read spec section 3 before starting.

**Files:**
- Create: `web/src/features/pos/utils/phase.ts`
- Test: `web/src/features/pos/utils/phase.test.ts`

**Interfaces:**
- Consumes: `SalesServiceSessionResponse`, `SalesCheckResponse` from `@/api/generated/models`.
- Produces:
  - `type PosPhase = "NO_SESSION" | "DRAFTING" | "AWAITING_PAYMENT" | "SETTLED"`
  - `derivePosPhase(session: SalesServiceSessionResponse | null | undefined): PosPhase`
  - `listLiveChecks(session): SalesCheckResponse[]`
  - `selectOpenCheck(session): SalesCheckResponse | null`
  - `hasMultipleOpenChecks(session): boolean`
  - `findCheckById(session, checkId: string): SalesCheckResponse | null`

- [ ] **Step 1: Write the failing test**

Create `web/src/features/pos/utils/phase.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import {
  derivePosPhase,
  listLiveChecks,
  selectOpenCheck,
  hasMultipleOpenChecks,
  findCheckById,
} from "./phase";
import type { SalesServiceSessionResponse } from "@/api/generated/models";

function session(
  overrides: Partial<SalesServiceSessionResponse>,
): SalesServiceSessionResponse {
  return {
    id: "session-1",
    service_number: "007",
    service_mode: "TAKEAWAY",
    state: "ACTIVE",
    sales_shift_id: "shift-1",
    tables: [],
    checks: [],
    orders: [],
    preparation_units: [],
    ...overrides,
  };
}

const editableDraft = { id: "draft-1", state: "EDITABLE", check_target: "SAME_CHECK", items: [] };

describe("derivePosPhase", () => {
  it("reports NO_SESSION without a session", () => {
    expect(derivePosPhase(null)).toBe("NO_SESSION");
    expect(derivePosPhase(undefined)).toBe("NO_SESSION");
  });

  it("reports DRAFTING while an editable draft stands", () => {
    expect(derivePosPhase(session({ draft: editableDraft }))).toBe("DRAFTING");
  });

  it("reports AWAITING_PAYMENT once the draft is committed and a Check is open", () => {
    const committed = session({
      checks: [{ id: "check-1", state: "OPEN", balance_vnd: 47_000, created_at: "2026-09-24T01:00:00Z" }],
    });
    expect(derivePosPhase(committed)).toBe("AWAITING_PAYMENT");
  });

  it("reports SETTLED once every Check is settled", () => {
    const settled = session({
      checks: [{ id: "check-1", state: "SETTLED", balance_vnd: 0, total_applied_vnd: 47_000 }],
    });
    expect(derivePosPhase(settled)).toBe("SETTLED");
  });

  it("reports AWAITING_PAYMENT when a settled Check sits beside an open one", () => {
    const mixed = session({
      checks: [
        { id: "check-1", state: "SETTLED", balance_vnd: 0 },
        { id: "check-2", state: "OPEN", balance_vnd: 12_000 },
      ],
    });
    expect(derivePosPhase(mixed)).toBe("AWAITING_PAYMENT");
  });

  it("falls back to NO_SESSION when the draft is gone and no Check exists", () => {
    expect(derivePosPhase(session({ checks: [] }))).toBe("NO_SESSION");
  });

  it("ignores a Check that was merged away", () => {
    const merged = session({
      checks: [
        { id: "check-1", state: "MERGED", merged_into_check_id: "check-2" },
        { id: "check-2", state: "SETTLED", balance_vnd: 0 },
      ],
    });
    expect(derivePosPhase(merged)).toBe("SETTLED");
  });
});

describe("selectOpenCheck", () => {
  it("returns the oldest open Check", () => {
    const s = session({
      checks: [
        { id: "newer", state: "OPEN", created_at: "2026-09-24T03:00:00Z" },
        { id: "older", state: "OPEN", created_at: "2026-09-24T01:00:00Z" },
      ],
    });
    expect(selectOpenCheck(s)?.id).toBe("older");
  });

  it("returns null when nothing is open", () => {
    expect(selectOpenCheck(session({ checks: [{ id: "c", state: "SETTLED" }] }))).toBeNull();
    expect(selectOpenCheck(null)).toBeNull();
  });
});

describe("hasMultipleOpenChecks", () => {
  it("is true only with more than one open Check", () => {
    const one = session({ checks: [{ id: "a", state: "OPEN" }] });
    const two = session({ checks: [{ id: "a", state: "OPEN" }, { id: "b", state: "OPEN" }] });
    expect(hasMultipleOpenChecks(one)).toBe(false);
    expect(hasMultipleOpenChecks(two)).toBe(true);
  });
});

describe("listLiveChecks and findCheckById", () => {
  it("drops merged Checks and finds by id", () => {
    const s = session({
      checks: [
        { id: "gone", state: "MERGED", merged_into_check_id: "kept" },
        { id: "kept", state: "SETTLED" },
      ],
    });
    expect(listLiveChecks(s).map((c) => c.id)).toEqual(["kept"]);
    expect(findCheckById(s, "kept")?.id).toBe("kept");
    expect(findCheckById(s, "gone")).toBeNull();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/pos/utils/phase.test.ts`
Expected: FAIL — module `./phase` not found.

- [ ] **Step 3: Write the implementation**

Create `web/src/features/pos/utils/phase.ts`:

```ts
import type {
  SalesCheckResponse,
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
export type PosPhase = "NO_SESSION" | "DRAFTING" | "AWAITING_PAYMENT" | "SETTLED";

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

export function derivePosPhase(session: MaybeSession): PosPhase {
  if (!session) return "NO_SESSION";

  if (session.draft && session.draft.state === "EDITABLE") return "DRAFTING";

  const checks = listLiveChecks(session);

  // Unreachable by the domain: a committed draft always charges a Check.
  // Answering NO_SESSION keeps the terminal usable instead of blank.
  if (checks.length === 0) return "NO_SESSION";

  if (selectOpenCheck(session)) return "AWAITING_PAYMENT";

  return "SETTLED";
}
```

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/features/pos/utils/phase.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/pos/utils/phase.ts web/src/features/pos/utils/phase.test.ts
git commit -m "feat(web): derive pos phase from the service session projection"
```

---

## Task 4: Vietnamese error messages

**Files:**
- Modify: `web/src/lib/error-messages.ts:33` (after `INVALID_QUANTITY`)
- Test: `web/src/lib/error-messages.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: 18 new keys in the exported `ERROR_MESSAGES` record; `messageForError` is unchanged.

- [ ] **Step 1: Write the failing test**

Append to `web/src/lib/error-messages.test.ts`:

```ts
describe("commit and payment error codes", () => {
  const codes = [
    "EMPTY_DRAFT",
    "COMMIT_MENU_ITEM_UNAVAILABLE",
    "COMMIT_MENU_ITEM_RETIRED",
    "COMMIT_SIZE_REQUIRED",
    "COMMIT_SIZE_INVALID",
    "COMMIT_SIZE_UNAVAILABLE",
    "COMMIT_SIZE_RETIRED",
    "COMMIT_MODIFIER_OPTION_INVALID",
    "COMMIT_MODIFIER_OPTION_UNAVAILABLE",
    "COMMIT_MODIFIER_OPTION_RETIRED",
    "COMMIT_MODIFIER_GROUP_INVALID",
    "COMMIT_MODIFIER_GROUP_RETIRED",
    "NEW_ORDER_DRAFT_NOT_AVAILABLE",
    "CHECK_NOT_FOUND",
    "CHECK_NOT_OPEN",
    "CHECK_HAS_PAYMENT",
    "PAYMENT_EXCEEDS_CHECK_BALANCE",
    "INSUFFICIENT_CASH_TENDERED",
  ];

  it("translates every commit and payment code", () => {
    for (const code of codes) {
      expect(ERROR_MESSAGES[code]).toBeString();
      expect(ERROR_MESSAGES[code].length).toBeGreaterThan(0);
    }
  });

  it("reads the payment shortfall message through messageForError", () => {
    const error = new ApiError(409, "INSUFFICIENT_CASH_TENDERED", "cash tendered is below");
    expect(messageForError(error)).toBe("Tiền khách đưa ít hơn số tiền cần thu.");
  });
});
```

Reuse the file's existing imports of `ERROR_MESSAGES`, `messageForError`, and `ApiError`; add any that are missing to the existing import statements.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/lib/error-messages.test.ts`
Expected: FAIL — the codes are undefined.

- [ ] **Step 3: Add the messages**

Insert into the `ERROR_MESSAGES` object in `web/src/lib/error-messages.ts`, directly after the `INVALID_QUANTITY` entry:

```ts
  EMPTY_DRAFT: "Đơn chưa có món nào để thanh toán.",
  COMMIT_MENU_ITEM_UNAVAILABLE:
    "Một món trong đơn vừa được tạm ngưng phục vụ. Vui lòng kiểm tra lại đơn.",
  COMMIT_MENU_ITEM_RETIRED:
    "Một món trong đơn đã ngừng kinh doanh. Vui lòng xóa món đó khỏi đơn.",
  COMMIT_SIZE_REQUIRED: "Một món trong đơn chưa chọn kích cỡ.",
  COMMIT_SIZE_INVALID: "Kích cỡ của một món trong đơn không còn hợp lệ.",
  COMMIT_SIZE_UNAVAILABLE: "Kích cỡ của một món trong đơn vừa tạm hết.",
  COMMIT_SIZE_RETIRED: "Kích cỡ của một món trong đơn đã ngừng phục vụ.",
  COMMIT_MODIFIER_OPTION_INVALID: "Tùy chọn topping của một món không còn hợp lệ.",
  COMMIT_MODIFIER_OPTION_UNAVAILABLE: "Tùy chọn topping của một món vừa tạm hết.",
  COMMIT_MODIFIER_OPTION_RETIRED: "Tùy chọn topping của một món đã ngừng phục vụ.",
  COMMIT_MODIFIER_GROUP_INVALID:
    "Lựa chọn topping của một món không thỏa quy định của nhóm.",
  COMMIT_MODIFIER_GROUP_RETIRED: "Một nhóm topping bắt buộc đã ngừng áp dụng.",
  NEW_ORDER_DRAFT_NOT_AVAILABLE:
    "Đơn trước chưa được gửi bếp nên chưa thể mở đơn mới.",
  CHECK_NOT_FOUND: "Không tìm thấy hóa đơn.",
  CHECK_NOT_OPEN: "Hóa đơn này không còn ở trạng thái chờ thu tiền.",
  CHECK_HAS_PAYMENT: "Hóa đơn này đã được thanh toán.",
  PAYMENT_EXCEEDS_CHECK_BALANCE: "Số tiền thu vượt quá số còn phải thu của hóa đơn.",
  INSUFFICIENT_CASH_TENDERED: "Tiền khách đưa ít hơn số tiền cần thu.",
```

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/lib/error-messages.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/error-messages.ts web/src/lib/error-messages.test.ts
git commit -m "feat(web): add commit and cash payment error translations"
```

---

## Task 5: Checkout API seam

**Files:**
- Create: `web/src/features/pos/api/use-checkout.ts`
- Test: `web/src/features/pos/api/use-checkout.test.ts`

**Interfaces:**
- Consumes: `unwrap` from `@/lib/unwrap`, `newRequestId` from `@/lib/command`.
- Produces:
  - `useCommitDraft(sessionId: string)` returning the mutation object plus
    `commitDraft(requestId?: string, targetSessionId?: string): Promise<SalesServiceSessionResponse>`
  - `usePayCash(sessionId: string)` returning the mutation object plus
    `payCash(checkId: string, command: SalesPayCashCommand, requestId?: string, targetSessionId?: string): Promise<SalesServiceSessionResponse>`

Both write the returned projection into the session query cache, exactly as the Slice 3 hooks in `use-pos.ts` do.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/pos/api/use-checkout.test.ts`:

```ts
import { describe, expect, it } from "bun:test";
import { useCommitDraft, usePayCash } from "./use-checkout";

describe("use-checkout API seam exports", () => {
  it("exports the commit and cash payment hooks", () => {
    expect(typeof useCommitDraft).toBe("function");
    expect(typeof usePayCash).toBe("function");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/pos/api/use-checkout.test.ts`
Expected: FAIL — module `./use-checkout` not found.

- [ ] **Step 3: Write the implementation**

Create `web/src/features/pos/api/use-checkout.ts`:

```ts
import { useQueryClient } from "@tanstack/react-query";
import {
  usePostSalesServiceSessionsIdDraftCommit,
  usePostSalesChecksCheckIdPaymentsCash,
  getGetSalesServiceSessionsIdQueryKey,
} from "@/api/generated/endpoints/sales/sales";
import { unwrap } from "@/lib/unwrap";
import { newRequestId } from "@/lib/command";
import type { SalesPayCashCommand } from "@/api/generated/models";

/**
 * Commits the Order Draft: the server revalidates it, freezes prices into
 * immutable Committed Items, and charges a Check.
 *
 * The response carries the new Check with the amount actually owed, which is
 * the only figure the following payment may apply. Client-side pricing is a
 * display estimate and must never reach the wire.
 */
export function useCommitDraft(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostSalesServiceSessionsIdDraftCommit();

  return {
    ...mutation,
    commitDraft: async (requestId?: string, targetSessionId?: string) => {
      const sid = targetSessionId || sessionId;
      const rid = requestId ?? newRequestId();
      const res = await mutation.mutateAsync({ id: sid, data: { request_id: rid } });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sid), res);
      return data;
    },
  };
}

/**
 * Records cash against one Check. The Check settles in the same transaction
 * when the payment brings its balance to zero.
 */
export function usePayCash(sessionId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostSalesChecksCheckIdPaymentsCash();

  return {
    ...mutation,
    payCash: async (
      checkId: string,
      command: SalesPayCashCommand,
      requestId?: string,
      targetSessionId?: string,
    ) => {
      const sid = targetSessionId || sessionId;
      const rid = requestId ?? command.request_id ?? newRequestId();
      const res = await mutation.mutateAsync({
        checkId,
        data: { ...command, request_id: rid },
      });
      const data = unwrap(res);
      queryClient.setQueryData(getGetSalesServiceSessionsIdQueryKey(sid), res);
      return data;
    },
  };
}
```

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/features/pos/api/use-checkout.test.ts && tsc -b`
Expected: PASS and a clean typecheck.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/pos/api/use-checkout.ts web/src/features/pos/api/use-checkout.test.ts
git commit -m "feat(web): add commit and pay-cash api seam"
```

---

## Task 6: Extract the session lifecycle

A pure refactor with no behaviour change. `pos-view.tsx` is 403 lines; the checkout orchestration in Task 9 would push it past 550 unless the session plumbing moves out first.

**Files:**
- Create: `web/src/features/pos/api/use-pos-session.ts`
- Modify: `web/src/features/pos/components/pos-view.tsx:31-135`

**Interfaces:**
- Consumes: `useServiceSession`, `useStartTakeawaySession` from `./use-pos`.
- Produces: `usePosSession(): PosSessionHandle` where

```ts
export interface PosSessionHandle {
  activeSessionId: string | null;
  session: SalesServiceSessionResponse | null;
  isSessionError: boolean;
  ensureSessionId: () => Promise<string>;
  clearSession: () => void;
}
```

`clearSession()` is new: Task 9's "Khách tiếp theo" uses it to drop the settled Session and start the next customer clean.

- [ ] **Step 1: Write the new module**

Create `web/src/features/pos/api/use-pos-session.ts`, moving the logic verbatim out of `pos-view.tsx` lines 31 and 45-135:

```ts
import * as React from "react";
import { useServiceSession, useStartTakeawaySession } from "./use-pos";
import type { SalesServiceSessionResponse } from "@/api/generated/models";

const STORAGE_SESSION_KEY = "pos_active_session_id";

export interface PosSessionHandle {
  activeSessionId: string | null;
  session: SalesServiceSessionResponse | null;
  isSessionError: boolean;
  ensureSessionId: () => Promise<string>;
  clearSession: () => void;
}

function readStoredSessionId(): string | null {
  try {
    return sessionStorage.getItem(STORAGE_SESSION_KEY);
  } catch {
    return null;
  }
}

function writeStoredSessionId(id: string | null): void {
  try {
    if (id === null) sessionStorage.removeItem(STORAGE_SESSION_KEY);
    else sessionStorage.setItem(STORAGE_SESSION_KEY, id);
  } catch {
    // ignore storage errors
  }
}

/**
 * Owns the single active Service Session pointer for this browser tab.
 *
 * The Session is opened lazily, on the first item added, so browsing the menu
 * never leaves an empty Session behind.
 */
export function usePosSession(): PosSessionHandle {
  const [activeSessionId, setActiveSessionId] = React.useState<string | null>(
    readStoredSessionId,
  );

  const activeSessionIdRef = React.useRef<string | null>(activeSessionId);
  React.useEffect(() => {
    activeSessionIdRef.current = activeSessionId;
  }, [activeSessionId]);

  const creatingSessionPromiseRef = React.useRef<Promise<string> | null>(null);

  const { data: session, isError: isSessionError } = useServiceSession(activeSessionId);
  const { startTakeaway } = useStartTakeawaySession();

  const clearSession = React.useCallback(() => {
    writeStoredSessionId(null);
    activeSessionIdRef.current = null;
    setActiveSessionId(null);
  }, []);

  // Drop the pointer when the Session is gone or no longer active.
  React.useEffect(() => {
    if ((session && session.state && session.state !== "ACTIVE") || isSessionError) {
      writeStoredSessionId(null);
      activeSessionIdRef.current = null;
      // oxlint-disable-next-line react/set-state-in-effect
      setActiveSessionId(null);
    }
  }, [session, isSessionError]);

  const ensureSessionId = React.useCallback(async (): Promise<string> => {
    if (activeSessionIdRef.current) return activeSessionIdRef.current;
    if (creatingSessionPromiseRef.current) return await creatingSessionPromiseRef.current;

    const promise = (async () => {
      try {
        const newSession = await startTakeaway();
        if (!newSession.id) {
          throw new Error("Không thể khởi tạo phiên phục vụ: thiếu mã phiên");
        }
        const id = newSession.id;
        writeStoredSessionId(id);
        activeSessionIdRef.current = id;
        setActiveSessionId(id);
        return id;
      } finally {
        creatingSessionPromiseRef.current = null;
      }
    })();

    creatingSessionPromiseRef.current = promise;
    return await promise;
  }, [startTakeaway]);

  return {
    activeSessionId,
    session: session ?? null,
    isSessionError,
    ensureSessionId,
    clearSession,
  };
}
```

- [ ] **Step 2: Rewire `pos-view.tsx`**

Delete `const STORAGE_SESSION_KEY = ...` (line 31) and everything from the `// Single active session pointer in sessionStorage` comment through the closing brace of `ensureSessionId` (lines 45-135), including the `useServiceSession` and `useStartTakeawaySession` calls and the session-clearing effect. Replace with:

```tsx
const { activeSessionId, session, ensureSessionId, clearSession } = usePosSession();
```

Update the imports at the top: drop `useServiceSession` and `useStartTakeawaySession` from the `../api/use-pos` import, and add:

```tsx
import { usePosSession } from "../api/use-pos-session";
```

`session` is now `SalesServiceSessionResponse | null`, so change the `DraftPanel` prop from `session={session ?? null}` to `session={session}`. `clearSession` is unused until Task 9 — prefix it out of the destructure for now by not destructuring it, and add it back in Task 9.

- [ ] **Step 3: Verify nothing changed**

Run: `cd web && bun test && bun run lint && tsc -b`
Expected: every existing test still passes, including `src/features/pos/components/pos-view.test.tsx`.

- [ ] **Step 4: Commit**

```bash
git add web/src/features/pos/api/use-pos-session.ts web/src/features/pos/components/pos-view.tsx
git commit -m "refactor(web): extract pos service session lifecycle into a hook"
```

---

## Task 7: The payment dialog

Presentational only — it holds the tendered amount and nothing else. Orchestration lives in `pos-view.tsx` (Task 9), matching how `ItemPickerDialog` is built.

**Files:**
- Create: `web/src/features/pos/components/payment-dialog.tsx`
- Test: `web/src/features/pos/components/payment-dialog.test.tsx`

**Interfaces:**
- Consumes: `changeDue`, `isTenderSufficient`, `suggestTenders` (Task 2); `formatVND` from `@/lib/utils`; `useKeypadHotkeys` from `@/hooks/use-keypad-hotkeys`; `playTapChirp`, `playSuccessChirp` from `@/lib/sound`.
- Produces:

```ts
export interface PaymentDialogProps {
  isOpen: boolean;
  serviceNumber?: string;
  totalVnd: number;
  isCommitted: boolean;
  isSubmitting: boolean;
  errorMessage: string | null;
  changeDueVnd: number | null;
  onClose: () => void;
  onConfirm: (tenderedVnd: number) => void;
  onDone: () => void;
}
export function PaymentDialog(props: PaymentDialogProps): React.ReactElement | null;
```

`changeDueVnd !== null` switches the dialog to its result screen. `isCommitted` is true when the dialog was opened from the Check panel, where the commit already happened.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/pos/components/payment-dialog.test.tsx`:

```tsx
import { describe, it, expect } from "bun:test";
import { renderToString } from "react-dom/server";
import { PaymentDialog } from "./payment-dialog";

const base = {
  isOpen: true,
  serviceNumber: "007",
  totalVnd: 47_000,
  isCommitted: false,
  isSubmitting: false,
  errorMessage: null,
  changeDueVnd: null,
  onClose: () => {},
  onConfirm: () => {},
  onDone: () => {},
};

describe("PaymentDialog", () => {
  it("renders nothing while closed", () => {
    expect(renderToString(<PaymentDialog {...base} isOpen={false} />)).toBe("");
  });

  it("shows the total and the quick tender buttons", () => {
    const html = renderToString(<PaymentDialog {...base} />);
    expect(html).toContain("47.000");
    expect(html).toContain("Đúng tiền");
    expect(html).toContain("50.000");
    expect(html).toContain("100.000");
  });

  it("labels the confirm action for an uncommitted draft", () => {
    const html = renderToString(<PaymentDialog {...base} />);
    expect(html).toContain("Xác nhận");
  });

  it("announces the committed state when the draft is already charged", () => {
    const html = renderToString(<PaymentDialog {...base} isCommitted />);
    expect(html).toContain("Đã chốt đơn");
  });

  it("shows the change on the result screen", () => {
    const html = renderToString(<PaymentDialog {...base} changeDueVnd={3_000} />);
    expect(html).toContain("Tiền thối");
    expect(html).toContain("3.000");
    expect(html).toContain("Xong");
  });

  it("surfaces an error message", () => {
    const html = renderToString(
      <PaymentDialog {...base} errorMessage="Đơn chưa có món nào để thanh toán." />,
    );
    expect(html).toContain("Đơn chưa có món nào để thanh toán.");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/pos/components/payment-dialog.test.tsx`
Expected: FAIL — module `./payment-dialog` not found.

- [ ] **Step 3: Write the component**

Create `web/src/features/pos/components/payment-dialog.tsx`:

```tsx
import * as React from "react";
import { X, Banknote, AlertCircle, CheckCircle2 } from "lucide-react";
import { changeDue, isTenderSufficient, suggestTenders } from "../utils/payment";
import { formatVND } from "@/lib/utils";
import { useKeypadHotkeys } from "@/hooks/use-keypad-hotkeys";
import { playTapChirp, playSuccessChirp } from "@/lib/sound";

export interface PaymentDialogProps {
  isOpen: boolean;
  serviceNumber?: string;
  /** The amount owed: the draft subtotal before commit, the Check balance after. */
  totalVnd: number;
  /** True when the draft is already committed, so confirming only takes money. */
  isCommitted: boolean;
  isSubmitting: boolean;
  errorMessage: string | null;
  /** Server-reported change. Non-null switches the dialog to its result screen. */
  changeDueVnd: number | null;
  onClose: () => void;
  onConfirm: (tenderedVnd: number) => void;
  onDone: () => void;
}

function parseTendered(raw: string): number {
  const digits = raw.replace(/\D/g, "");
  if (digits === "") return 0;
  return Number.parseInt(digits, 10);
}

export function PaymentDialog({
  isOpen,
  serviceNumber,
  totalVnd,
  isCommitted,
  isSubmitting,
  errorMessage,
  changeDueVnd,
  onClose,
  onConfirm,
  onDone,
}: PaymentDialogProps) {
  const [rawTendered, setRawTendered] = React.useState("");
  const prevIsOpenRef = React.useRef(isOpen);

  // Reset the entry each time the dialog opens, never while it is open.
  React.useEffect(() => {
    if (isOpen && !prevIsOpenRef.current) {
      // oxlint-disable-next-line react/set-state-in-effect
      setRawTendered("");
    }
    prevIsOpenRef.current = isOpen;
  }, [isOpen]);

  const isPaid = changeDueVnd !== null;
  const tendered = parseTendered(rawTendered);
  const canConfirm = isTenderSufficient(tendered, totalVnd) && !isSubmitting;

  const handleConfirm = () => {
    if (isPaid) {
      playSuccessChirp();
      onDone();
      return;
    }
    if (!canConfirm) return;
    playTapChirp();
    onConfirm(tendered);
  };

  const handleClose = () => {
    if (isSubmitting) return;
    playTapChirp();
    if (isPaid) onDone();
    else onClose();
  };

  // Digits, Backspace and C fire only when focus is outside the input, so the
  // field types naturally while the dialog still behaves like a keypad.
  useKeypadHotkeys({
    onDigit: (digit) => setRawTendered((prev) => prev + digit),
    onBackspace: () => setRawTendered((prev) => prev.slice(0, -1)),
    onClear: () => setRawTendered(""),
    onSubmit: handleConfirm,
    onClose: handleClose,
    enabled: isOpen,
  });

  if (!isOpen) return null;

  const shortfall = Math.max(0, totalVnd - tendered);
  const previewChange = changeDue(tendered, totalVnd);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-xs animate-in fade-in duration-150">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="payment-dialog-title"
        className="flex flex-col w-full max-w-md rounded-2xl border border-border bg-card shadow-2xl overflow-hidden animate-in fade-in zoom-in-95 duration-150"
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border p-4 bg-muted/20">
          <div className="flex items-center gap-3">
            <div className="h-10 w-10 rounded-xl bg-primary/10 text-primary flex items-center justify-center">
              <Banknote className="h-5 w-5" />
            </div>
            <div>
              <h3 id="payment-dialog-title" className="text-base font-bold text-foreground">
                Thanh toán
              </h3>
              <p className="text-2xs text-muted-foreground">
                {serviceNumber ? `Đơn mang đi #${serviceNumber}` : "Đơn mang đi"}
                {isCommitted ? " · Đã chốt đơn" : ""}
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={handleClose}
            aria-label="Đóng"
            className="h-12 w-12 min-h-[48px] min-w-[48px] rounded-xl text-muted-foreground hover:text-foreground hover:bg-muted flex items-center justify-center select-none active:scale-[0.98] transition"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {isPaid ? (
          /* Result screen: the cashier counts the change off this number. */
          <div className="p-6 space-y-4 text-center">
            <div className="mx-auto h-12 w-12 rounded-2xl bg-primary/10 text-primary flex items-center justify-center">
              <CheckCircle2 className="h-6 w-6" />
            </div>
            <p className="text-sm font-bold text-foreground">Đã thu tiền</p>
            <div className="space-y-1">
              <p className="text-xs uppercase tracking-wider text-muted-foreground">
                Tiền thối
              </p>
              <p className="font-mono text-4xl font-bold text-primary tabular-nums">
                {formatVND(changeDueVnd)}
              </p>
            </div>
          </div>
        ) : (
          <div className="p-5 space-y-5">
            {/* Amount owed */}
            <div className="rounded-xl border border-border bg-muted/20 p-4 text-center space-y-1">
              <p className="text-xs uppercase tracking-wider text-muted-foreground">
                Tổng cộng
              </p>
              <p className="font-mono text-3xl font-bold text-primary tabular-nums">
                {formatVND(totalVnd)}
              </p>
            </div>

            {/* Quick tender */}
            <div className="grid grid-cols-2 gap-2">
              {suggestTenders(totalVnd).map((amount, index) => (
                <button
                  key={amount}
                  type="button"
                  onClick={() => {
                    playTapChirp();
                    setRawTendered(String(amount));
                  }}
                  className="min-h-[48px] rounded-xl border border-border bg-card px-3 text-sm font-bold text-foreground hover:bg-muted select-none active:scale-[0.98] transition"
                >
                  {index === 0 ? "Đúng tiền" : formatVND(amount)}
                </button>
              ))}
            </div>

            {/* Tendered entry */}
            <div className="space-y-1.5">
              <label
                htmlFor="cash-tendered"
                className="text-xs font-bold uppercase tracking-wider text-muted-foreground"
              >
                Tiền khách đưa
              </label>
              <input
                id="cash-tendered"
                inputMode="numeric"
                autoFocus
                value={rawTendered}
                onChange={(e) => setRawTendered(e.target.value.replace(/\D/g, ""))}
                placeholder="0"
                className="w-full min-h-[48px] rounded-xl border border-border bg-card px-4 font-mono text-2xl font-bold text-foreground tabular-nums text-right outline-none focus:border-primary"
              />
            </div>

            {/* Change preview */}
            <div className="flex items-baseline justify-between rounded-xl bg-muted/30 px-4 py-3">
              {shortfall > 0 ? (
                <>
                  <span className="text-sm font-bold text-destructive">Còn thiếu</span>
                  <span className="font-mono text-xl font-bold text-destructive tabular-nums">
                    {formatVND(shortfall)}
                  </span>
                </>
              ) : (
                <>
                  <span className="text-sm font-bold text-foreground">Tiền thối</span>
                  <span className="font-mono text-xl font-bold text-primary tabular-nums">
                    {formatVND(previewChange)}
                  </span>
                </>
              )}
            </div>
          </div>
        )}

        {errorMessage && (
          <div
            role="alert"
            className="mx-5 mb-4 flex items-start gap-2 rounded-xl bg-destructive/10 text-destructive px-3 py-2.5 text-xs font-semibold"
          >
            <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
            <span>{errorMessage}</span>
          </div>
        )}

        {/* Footer */}
        <div className="border-t border-border bg-muted/20 p-4 flex gap-2">
          {!isPaid && (
            <button
              type="button"
              onClick={handleClose}
              disabled={isSubmitting}
              className="min-h-[48px] flex-1 rounded-xl border border-border bg-card text-sm font-bold text-foreground hover:bg-muted disabled:opacity-50 select-none active:scale-[0.98] transition"
            >
              Hủy (Esc)
            </button>
          )}
          <button
            type="button"
            onClick={handleConfirm}
            disabled={!isPaid && !canConfirm}
            className="min-h-[48px] flex-1 rounded-xl bg-primary text-sm font-bold text-primary-foreground disabled:opacity-50 disabled:cursor-not-allowed select-none active:scale-[0.98] transition"
          >
            {isPaid ? "Xong (Enter)" : isSubmitting ? "Đang xử lý..." : "Xác nhận (Enter)"}
          </button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/features/pos/components/payment-dialog.test.tsx && tsc -b`
Expected: PASS and a clean typecheck.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/pos/components/payment-dialog.tsx web/src/features/pos/components/payment-dialog.test.tsx
git commit -m "feat(web): add cash payment dialog for pos checkout"
```

---

## Task 8: The Check panel

**Files:**
- Create: `web/src/features/pos/components/check-panel.tsx`
- Test: `web/src/features/pos/components/check-panel.test.tsx`

**Interfaces:**
- Consumes: `selectOpenCheck`, `listLiveChecks`, `hasMultipleOpenChecks`, `PosPhase` (Task 3); `latestPaymentChangeDue` (Task 2); `formatVND`.
- Produces:

```ts
export interface CheckPanelProps {
  session: SalesServiceSessionResponse | null;
  phase: PosPhase;
  onCollect: () => void;
  onNextCustomer: () => void;
  className?: string;
}
export function CheckPanel(props: CheckPanelProps): React.ReactElement;
```

- [ ] **Step 1: Write the failing test**

Create `web/src/features/pos/components/check-panel.test.tsx`:

```tsx
import { describe, it, expect } from "bun:test";
import { renderToString } from "react-dom/server";
import { CheckPanel } from "./check-panel";
import type { SalesServiceSessionResponse } from "@/api/generated/models";

function session(checks: SalesServiceSessionResponse["checks"]): SalesServiceSessionResponse {
  return {
    id: "session-1",
    service_number: "007",
    service_mode: "TAKEAWAY",
    state: "ACTIVE",
    sales_shift_id: "shift-1",
    tables: [],
    checks,
    orders: [],
    preparation_units: [],
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
    },
  ],
};

describe("CheckPanel", () => {
  it("lists the committed items and the outstanding balance", () => {
    const html = renderToString(
      <CheckPanel
        session={session([openCheck])}
        phase="AWAITING_PAYMENT"
        onCollect={() => {}}
        onNextCustomer={() => {}}
      />,
    );
    expect(html).toContain("Đã chốt");
    expect(html).toContain("Cà phê sữa đá");
    expect(html).toContain("Vừa (M)");
    expect(html).toContain("Còn phải thu");
    expect(html).toContain("47.000");
    expect(html).toContain("Thu tiền (F9)");
  });

  it("shows the settled state with the change given", () => {
    const settled = {
      ...openCheck,
      state: "SETTLED",
      balance_vnd: 0,
      total_applied_vnd: 47_000,
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
    };
    const html = renderToString(
      <CheckPanel
        session={session([settled])}
        phase="SETTLED"
        onCollect={() => {}}
        onNextCustomer={() => {}}
      />,
    );
    expect(html).toContain("Đã thanh toán");
    expect(html).toContain("Tiền thối");
    expect(html).toContain("3.000");
    expect(html).toContain("Khách tiếp theo (F9)");
    expect(html).toContain("Mở ở Slice 5");
  });

  it("refuses to collect when more than one Check is open", () => {
    const html = renderToString(
      <CheckPanel
        session={session([openCheck, { ...openCheck, id: "check-2" }])}
        phase="AWAITING_PAYMENT"
        onCollect={() => {}}
        onNextCustomer={() => {}}
      />,
    );
    expect(html).toContain("nhiều hóa đơn");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/pos/components/check-panel.test.tsx`
Expected: FAIL — module `./check-panel` not found.

- [ ] **Step 3: Write the component**

Create `web/src/features/pos/components/check-panel.tsx`:

```tsx
import { Receipt, AlertTriangle, CheckCircle2, ChefHat } from "lucide-react";
import type { SalesServiceSessionResponse } from "@/api/generated/models";
import {
  listLiveChecks,
  selectOpenCheck,
  hasMultipleOpenChecks,
  type PosPhase,
} from "../utils/phase";
import { latestPaymentChangeDue } from "../utils/payment";
import { formatVND } from "@/lib/utils";

export interface CheckPanelProps {
  session: SalesServiceSessionResponse | null;
  phase: PosPhase;
  onCollect: () => void;
  onNextCustomer: () => void;
  className?: string;
}

/**
 * The Order Bill once the draft is committed: a read-only Check, then the
 * settled receipt.
 *
 * Every amount here is the server's frozen snapshot. Nothing on this panel is
 * recomputed from the catalog, because the customer is charged what the Check
 * says and not what the menu says today.
 */
export function CheckPanel({
  session,
  phase,
  onCollect,
  onNextCustomer,
  className,
}: CheckPanelProps) {
  const asideLayout =
    className ?? "w-full md:w-[380px] lg:w-[420px] shrink-0 border-l border-border";

  const isSettled = phase === "SETTLED";
  const openCheck = selectOpenCheck(session);
  const checks = listLiveChecks(session);
  const check = openCheck ?? checks[0] ?? null;
  const ambiguous = hasMultipleOpenChecks(session);

  const allocations = check?.allocations ?? [];
  const totalApplied = checks.reduce((sum, c) => sum + (c.total_applied_vnd ?? 0), 0);
  const changeGiven = latestPaymentChangeDue(check);
  const serviceNumber = session?.service_number;

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
              <span className="rounded-md bg-emerald-100 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300 border border-emerald-200/60 px-2 py-0.5 text-2xs font-bold">
                {isSettled ? "Đã thanh toán" : "Đã chốt"}
              </span>
            </div>
            <p className="text-2xs text-muted-foreground mt-0.5">
              {isSettled ? "Đơn đã thu đủ tiền" : "Đơn đã chốt giá, chờ thu tiền"}
            </p>
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

      {/* Financial summary */}
      <div className="border-t border-border bg-card p-4 space-y-1.5 shrink-0 shadow-2xs">
        {isSettled ? (
          <>
            <div className="flex items-baseline justify-between text-xs text-muted-foreground">
              <span>Đã thu</span>
              <span className="font-mono font-semibold text-foreground">
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

      {/* Actions */}
      <div className="border-t border-border bg-muted/20 p-4 space-y-2.5 shrink-0">
        {isSettled ? (
          <>
            <button
              type="button"
              onClick={onNextCustomer}
              className="min-h-[48px] h-12 w-full rounded-xl bg-primary text-sm font-bold text-primary-foreground flex items-center justify-center gap-2 select-none active:scale-[0.98] transition"
            >
              <CheckCircle2 className="h-4 w-4" />
              <span>Khách tiếp theo (F9)</span>
            </button>
            <button
              type="button"
              disabled
              title="Gửi bếp sẽ hoạt động ở Slice 5"
              className="min-h-[48px] h-12 w-full rounded-xl bg-muted text-muted-foreground font-bold text-sm cursor-not-allowed border border-border flex flex-col items-center justify-center opacity-60"
            >
              <span className="flex items-center gap-2">
                <ChefHat className="h-4 w-4" />
                Gửi bếp
              </span>
              <span className="text-2xs font-normal text-muted-foreground">
                Mở ở Slice 5
              </span>
            </button>
          </>
        ) : (
          <button
            type="button"
            onClick={onCollect}
            disabled={ambiguous || !openCheck}
            className="min-h-[48px] h-12 w-full rounded-xl bg-primary text-sm font-bold text-primary-foreground disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center select-none active:scale-[0.98] transition"
          >
            Thu tiền (F9)
          </button>
        )}
      </div>
    </aside>
  );
}
```

- [ ] **Step 4: Run the tests**

Run: `cd web && bun test src/features/pos/components/check-panel.test.tsx && tsc -b`
Expected: PASS and a clean typecheck.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/pos/components/check-panel.tsx web/src/features/pos/components/check-panel.test.tsx
git commit -m "feat(web): add committed check panel for pos checkout"
```

---

## Task 9: Wire the checkout into the terminal

**Files:**
- Modify: `web/src/features/pos/components/draft-panel.tsx:12-20, 110-151`
- Modify: `web/src/features/pos/components/draft-panel.test.tsx`
- Modify: `web/src/features/pos/components/pos-view.tsx`

**Interfaces:**
- Consumes: everything from Tasks 2, 3, 5, 6, 7, 8.
- Produces: `DraftPanelProps` gains `onCheckout: () => void` and `canCheckout: boolean`.

- [ ] **Step 1: Write the failing test**

Append to `web/src/features/pos/components/draft-panel.test.tsx`, reusing that file's existing mock session and item fixtures:

```tsx
describe("DraftPanel checkout action", () => {
  it("enables the checkout button when the draft has items", () => {
    const html = renderToString(
      <DraftPanel
        session={mockSessionWithItems}
        isShiftOpen
        onEditItem={() => {}}
        onQuantityChange={() => {}}
        onRemoveItem={() => {}}
        onCheckout={() => {}}
        canCheckout
      />,
    );
    expect(html).toContain("Thanh toán (F9)");
    expect(html).not.toContain("Mở ở Slice 4");
  });

  it("keeps the VietQR tab disabled", () => {
    const html = renderToString(
      <DraftPanel
        session={mockSessionWithItems}
        isShiftOpen
        onEditItem={() => {}}
        onQuantityChange={() => {}}
        onRemoveItem={() => {}}
        onCheckout={() => {}}
        canCheckout
      />,
    );
    expect(html).toContain("VietQR");
    expect(html).toContain("Sắp có");
  });
});
```

`mockSessionWithItems` is the fixture already defined at the top of that file (line 45); reuse it rather than declaring a new one.

The file has five existing `<DraftPanel ... />` renders. `onCheckout` and `canCheckout` are required props, so all five must gain them or the typecheck fails. Pass `onCheckout={() => {}}` and `canCheckout={false}` to the existing five — none of them assert on the checkout button, so their expectations stay as they are.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && bun test src/features/pos/components/draft-panel.test.tsx`
Expected: FAIL — `onCheckout` is not a prop and the Slice 3 caption is still rendered.

- [ ] **Step 3: Update `draft-panel.tsx`**

Add to `DraftPanelProps` and the destructured parameters:

```tsx
  onCheckout: () => void;
  canCheckout: boolean;
```

Replace the payment tab block and the disabled CTA (the two blocks under the `{/* Payment Tabs (Disabled) */}` and `{/* Checkout Commitment CTA (Disabled in Slice 3) */}` comments) with:

```tsx
        {/* Payment method: cash only in Slice 4 */}
        <div className="grid grid-cols-2 gap-2 p-1 bg-muted rounded-xl border border-border">
          <div className="min-h-[40px] h-10 px-3 rounded-lg font-bold text-xs bg-card text-foreground flex items-center justify-center gap-1.5 shadow-2xs">
            <Banknote className="h-3.5 w-3.5" />
            <span>Tiền mặt</span>
          </div>
          <button
            type="button"
            disabled
            title="Thanh toán VietQR sẽ hoạt động ở phiên bản sau"
            className="min-h-[40px] h-10 px-3 rounded-lg font-medium text-xs text-muted-foreground opacity-50 cursor-not-allowed flex items-center justify-center gap-1.5"
          >
            <QrCode className="h-3.5 w-3.5" />
            <span>VietQR</span>
            <span className="text-2xs">Sắp có</span>
          </button>
        </div>

        {/* Checkout */}
        <button
          type="button"
          onClick={onCheckout}
          disabled={!canCheckout}
          className="min-h-[48px] h-12 w-full rounded-xl bg-primary text-sm font-bold text-primary-foreground disabled:bg-muted disabled:text-muted-foreground disabled:cursor-not-allowed flex items-center justify-center select-none active:scale-[0.98] transition"
        >
          <span>Thanh toán (F9)</span>
        </button>
```

Leave the mode switcher (`Mang đi` / `Tại bàn (F2)`) and the header's `Hủy đơn` button exactly as they are.

- [ ] **Step 4: Update `pos-view.tsx`**

Add the imports:

```tsx
import { useHotkeys } from "react-hotkeys-hook";
import { useCommitDraft, usePayCash } from "../api/use-checkout";
import { CheckPanel } from "./check-panel";
import { PaymentDialog } from "./payment-dialog";
import { derivePosPhase, selectOpenCheck, findCheckById } from "../utils/phase";
import { latestPaymentChangeDue } from "../utils/payment";
import { calculateDraftSubtotal } from "../utils/pricing";
import { newRequestId } from "@/lib/command";
```

Add `clearSession` back to the `usePosSession()` destructure, and add the checkout hooks beside the existing draft mutations:

```tsx
  const { commitDraft } = useCommitDraft(activeSessionId ?? "");
  const { payCash } = usePayCash(activeSessionId ?? "");
```

Add the checkout state beside the existing modal state:

```tsx
  const [isPaymentOpen, setIsPaymentOpen] = React.useState(false);
  const [isPaying, setIsPaying] = React.useState(false);
  const [paymentError, setPaymentError] = React.useState<string | null>(null);
  const [changeDueVnd, setChangeDueVnd] = React.useState<number | null>(null);

  // One request id per intent, retained across retries so a replay after a
  // network failure reproduces the original outcome instead of charging twice.
  const commitRequestIdRef = React.useRef<string | null>(null);
  const payRequestIdRef = React.useRef<string | null>(null);
```

Add the phase and the amounts, after the mutation hooks:

```tsx
  const phase = derivePosPhase(session);
  const draftItems = session?.draft?.items ?? [];
  const openCheck = selectOpenCheck(session);
  const paymentTotal =
    phase === "AWAITING_PAYMENT"
      ? (openCheck?.balance_vnd ?? 0)
      : calculateDraftSubtotal(draftItems);
```

Add the handlers after `handleRemoveItem`:

```tsx
  const openPaymentDialog = () => {
    if (!isShiftOpen && phase === "DRAFTING") return;
    if (phase !== "DRAFTING" && phase !== "AWAITING_PAYMENT") return;
    if (phase === "DRAFTING" && draftItems.length === 0) return;

    commitRequestIdRef.current = commitRequestIdRef.current ?? newRequestId();
    payRequestIdRef.current = payRequestIdRef.current ?? newRequestId();
    setPaymentError(null);
    setChangeDueVnd(null);
    setIsPaymentOpen(true);
  };

  const closePaymentDialog = () => {
    if (isPaying) return;
    setIsPaymentOpen(false);
    setPaymentError(null);
  };

  /**
   * Commit, then take the cash.
   *
   * The applied amount comes from the commit response, never from the
   * client-side subtotal: commit revalidates and freezes prices, so the Check
   * is the only authority on what is owed.
   */
  const handleConfirmPayment = async (tenderedVnd: number) => {
    if (!activeSessionId) return;
    setPaymentError(null);
    setIsPaying(true);

    try {
      let projection = session;

      if (phase === "DRAFTING") {
        projection = await commitDraft(commitRequestIdRef.current ?? undefined, activeSessionId);
      }

      const check = selectOpenCheck(projection);
      if (!check?.id) {
        throw new ApiError(0, "CHECK_NOT_FOUND", "Không tìm thấy hóa đơn vừa chốt");
      }

      const paid = await payCash(
        check.id,
        {
          applied_amount_vnd: check.balance_vnd ?? 0,
          cash_tendered_vnd: tenderedVnd,
        },
        payRequestIdRef.current ?? undefined,
        activeSessionId,
      );

      setChangeDueVnd(latestPaymentChangeDue(findCheckById(paid, check.id)));
      playSuccessChirp();
    } catch (err) {
      playErrorBuzz();
      const message = messageForError(err);
      // A failed commit leaves the draft editable, so the cashier belongs back
      // on the bill to fix whatever the server rejected.
      if (err instanceof ApiError && COMMIT_FAILURE_CODES.has(err.code)) {
        setIsPaymentOpen(false);
        setErrorMessage(message);
      } else {
        setPaymentError(message);
      }
    } finally {
      setIsPaying(false);
    }
  };

  const handlePaymentDone = () => {
    setIsPaymentOpen(false);
    setChangeDueVnd(null);
    setPaymentError(null);
    commitRequestIdRef.current = null;
    payRequestIdRef.current = null;
  };

  const handleNextCustomer = () => {
    commitRequestIdRef.current = null;
    payRequestIdRef.current = null;
    clearSession();
  };
```

Add the constant just below `STORAGE_SESSION_KEY`'s old position, at module scope:

```tsx
/** Commit-time revalidation failures that send the cashier back to the draft. */
const COMMIT_FAILURE_CODES = new Set([
  "EMPTY_DRAFT",
  "COMMIT_MENU_ITEM_UNAVAILABLE",
  "COMMIT_MENU_ITEM_RETIRED",
  "COMMIT_SIZE_REQUIRED",
  "COMMIT_SIZE_INVALID",
  "COMMIT_SIZE_UNAVAILABLE",
  "COMMIT_SIZE_RETIRED",
  "COMMIT_MODIFIER_OPTION_INVALID",
  "COMMIT_MODIFIER_OPTION_UNAVAILABLE",
  "COMMIT_MODIFIER_OPTION_RETIRED",
  "COMMIT_MODIFIER_GROUP_INVALID",
  "COMMIT_MODIFIER_GROUP_RETIRED",
]);
```

Extend the imports for the pieces the handlers use:

```tsx
import { ApiError } from "@/lib/unwrap";
import { playSuccessChirp, playErrorBuzz } from "@/lib/sound";
```

Bind F9. Place this with the other hooks, above the early returns:

```tsx
  useHotkeys(
    "f9",
    (event) => {
      event.preventDefault();
      if (isPaymentOpen) return;
      if (phase === "SETTLED") handleNextCustomer();
      else openPaymentDialog();
    },
    { enableOnFormTags: true },
  );
```

Swap the bill panel on phase. Replace the `<DraftPanel ... />` element inside the second `ResizablePanel` with:

```tsx
          {phase === "AWAITING_PAYMENT" || phase === "SETTLED" ? (
            <CheckPanel
              session={session}
              phase={phase}
              onCollect={openPaymentDialog}
              onNextCustomer={handleNextCustomer}
              className="w-full h-full flex-1"
            />
          ) : (
            <DraftPanel
              session={session}
              isShiftOpen={isShiftOpen}
              onEditItem={handleEditDraftItem}
              onQuantityChange={handleQuantityChange}
              onRemoveItem={handleRemoveItem}
              onCheckout={openPaymentDialog}
              canCheckout={isShiftOpen && draftItems.length > 0}
              className="w-full h-full flex-1"
            />
          )}
```

Render the dialog beside `ItemPickerDialog`:

```tsx
      <PaymentDialog
        isOpen={isPaymentOpen}
        serviceNumber={session?.service_number}
        totalVnd={paymentTotal}
        isCommitted={phase === "AWAITING_PAYMENT"}
        isSubmitting={isPaying}
        errorMessage={paymentError}
        changeDueVnd={changeDueVnd}
        onClose={closePaymentDialog}
        onConfirm={handleConfirmPayment}
        onDone={handlePaymentDone}
      />
```

- [ ] **Step 5: Run the whole suite**

Run: `cd web && bun test && bun run lint && tsc -b`
Expected: PASS throughout. If `pos-view.test.tsx` asserts the Slice 3 "Mở ở Slice 4" caption, update that assertion to expect the live button.

- [ ] **Step 6: Verify `pos-view.tsx` did not grow**

Run: `wc -l web/src/features/pos/components/pos-view.tsx`
Expected: fewer than 403 lines. If it is larger, move `handleConfirmPayment` and its siblings into a `useCheckoutFlow` hook in `web/src/features/pos/api/use-checkout.ts` before committing.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/pos/components/pos-view.tsx web/src/features/pos/components/draft-panel.tsx web/src/features/pos/components/draft-panel.test.tsx
git commit -m "feat(web): take cash payments from the pos terminal"
```

---

## Task 10: UAT gate

**Files:**
- Create: `.superpowers/sdd/2026-09-24-web-slice-4-pos-checkout/uat-handover.md`

- [ ] **Step 1: Run the full verification**

```bash
cd web && bun test && bun run lint && tsc -b && bun run build
```

Expected: all green. Record the test count.

- [ ] **Step 2: Write the handover**

Create `.superpowers/sdd/2026-09-24-web-slice-4-pos-checkout/uat-handover.md`:

```markdown
# Web Slice 4 UAT Handover: POS-b Commit, Check & Cash Payment

## Setup

1. `go run ./cmd/api` with a clean database.
2. `cd web && bun run scripts/dev-seed.ts` to seed the Manager and the menu.
3. `cd web && bun run dev`, sign in as `QL01`.
4. Open a Sales Shift from `/shift` with a starting float.

## Happy path

1. On the POS screen, add three items, at least one with a size and a topping.
2. Confirm the bill total.
3. Press **F9**.
4. Tap the **Đúng tiền** button. The change reads 0.
5. Clear the field and type an amount above the total. The change updates as you type.
6. Press **Enter**. The dialog shows **Tiền thối** with the change to hand back.
7. Press **Enter** again. The bill panel reads **Đã thanh toán**.
8. Press **F9**. A fresh empty bill appears for the next customer.

## Checks to make

- The change on the result screen matches the change shown while typing.
- The amount charged matches the bill total shown before F9.
- `Gửi bếp` is visible but disabled, captioned `Mở ở Slice 5`.
- The `VietQR` tab is visible but disabled.

## Unhappy paths

1. **Reload mid-payment.** Add items, press F9, confirm, and immediately reload
   the page while the request is in flight. The bill must come back either as an
   editable draft or as a committed Check showing the outstanding balance — never
   blank, and never as an editable draft with a Check already charged.
2. **An item goes away.** With items in the draft, open Admin in a second tab and
   mark one of them unavailable. Press F9 and confirm. The payment is refused, a
   Vietnamese message names the problem, and the draft is still editable.
3. **Network failure, then retry.** Add items, press F9, stop the Go server,
   confirm, restart the server, and press the confirm button again. Then open
   `/shift` and verify that exactly one cash payment was recorded for the amount.

## Known and accepted

- A settled Session stays `ACTIVE` and cannot be closed: Submit arrives in
  Slice 5. Taking the next customer opens a new Session rather than reusing this
  one. Recorded in the design spec, section 2.
```

- [ ] **Step 3: Commit**

```bash
git add .superpowers/sdd/2026-09-24-web-slice-4-pos-checkout/uat-handover.md
git commit -m "docs(web): add slice 4 uat handover script"
```

- [ ] **Step 4: Hand over**

Report the test count and the three unhappy paths to the user. Do not self-certify the slice: it is done when UAT passes.

---

## Self-Review Notes

Checked against the spec, section by section:

| Spec section | Task |
| :--- | :--- |
| 1.1.1 phase derivation | Task 3 |
| 1.1.2 checkout API seam | Task 5 |
| 1.1.3 payment dialog | Task 7 |
| 1.1.4 check panel | Task 8 |
| 1.1.5 payment arithmetic | Task 2 |
| 1.1.6 session extraction | Task 6 |
| 1.1.7 error mapping | Task 4 |
| 1.1.8 unit tests | Tasks 2, 3, 4, 5, 7, 8 |
| 2 accepted dead end | Task 8 (disabled `Gửi bếp`), Task 9 (`handleNextCustomer`), Task 10 (recorded) |
| 3.3 edge cases | Task 3 tests, Task 8 ambiguous-check warning |
| 4 file structure | File Structure table |
| 5.2 two-request sequence | Task 9 `handleConfirmPayment` |
| 5.3 idempotency | Task 9 request id refs |
| 5.4 commit-then-payment-failure | Task 9 (dialog stays open, `isCommitted` flips via derived phase) |
| 6 dialog | Task 7 |
| 7 check panel | Task 8 |
| 8 error handling | Task 4, Task 9 `COMMIT_FAILURE_CODES` |
| 9 testing | Tasks 2, 3, 7, 8 |
| 10 definition of done | Task 9 Step 6, Task 10 |

One deliberate refinement of the spec, recorded here rather than left implicit:
spec section 6.3 says the change shown comes from `payments[].change_due_vnd`.
Task 2's `latestPaymentChangeDue` picks the newest payment by `received_at`
rather than assuming a single payment, because a Check that was partially paid
before this slice existed would otherwise report the wrong figure.
