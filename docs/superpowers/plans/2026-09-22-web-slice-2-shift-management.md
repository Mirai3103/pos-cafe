# Web Slice 2: Sales Shift Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the placeholder shift page in `./web` with the real Sales Shift Management system — opening shifts, recording cash movements under Manager Approval, blind count reconciliation, recount/QR updates, and exact or discrepant closure.

**Architecture:** Server-authoritative shift state queried from `GET /shifts/current` via React Query and unwrapped with `unwrap()`. Client state is held in Zustand only for cross-cutting Manager Approval flow (`useManagerApprovalStore`). Each operator intent generates a durable `requestId` via `newRequestId()` retained across retries. The UI routes cleanly between three states: No Active Shift (`null`), Open Shift (`OPEN` with strict blind-count privacy), and Reconciliation (`CLOSING`).

**Tech Stack:** React 19, TanStack Router + Query v5, Zustand, Orval generated client (`customAxiosInstance`), Tailwind v4 with Crisp Emerald semantic tokens, Lucide icons, Web Audio sound feedback, `bun test`.

**Spec:** [`docs/superpowers/specs/2026-09-22-web-slice-2-shift-management-design.md`](../specs/2026-09-22-web-slice-2-shift-management-design.md)

---

## Global Constraints

- **No mock data survives in the shipped path.** Every screen and action calls the real API.
- **No end-to-end tests and no integration tests.** Basic tests only: pure logic, utilities, and store transitions, run with `bun test`.
- **The slice ends at a UAT gate** (Task 11). Completion is never self-certified.
- **Strict Blind Count Enforcement:** When `state === "OPEN"`, `GET /shifts/current` returns only metadata (`id`, `state`, `opened_at`, `opener`). The UI must NEVER display or assume expected cash, float, or revenue in `OPEN` state.
- **Single-Intent Idempotency:** Every mutation must pass a `request_id` generated per intent via `newRequestId()` and kept stable across retries until success or modal dismissal.
- **Manager Approval Dialog:** Built as a global modal managed by Zustand (`useManagerApprovalStore`), mounted in `_app.tsx`. Actions needing Manager Approval prompt for PIN via this dialog.
- **Semantic Tailwind tokens only:** `bg-primary`, `text-muted-foreground`, `border-border`, `bg-card`, `text-destructive`. No arbitrary pixel styles (`h-[48px]`, `bg-[#059669]`).
- **Touch targets are `h-12`** (`--spacing-touch: 3rem`, 48px). Button border radius default is `rounded-xl`.
- **Vietnamese copy only:** All labels, messages, and placeholders are Vietnamese.
- **Fonts:** `font-sans` (Outfit) for interface text, `font-mono` (JetBrains Mono) for PINs, IDs, currency, and quantities.

---

## File Structure

**Created:**

| File | Responsibility |
| :--- | :--- |
| `web/src/lib/command.ts` | `newRequestId()` helper generating UUID v4 for command idempotency. |
| `web/src/lib/command.test.ts` | Tests for `newRequestId()`. |
| `web/src/stores/use-manager-approval-store.ts` | Zustand store managing imperative Manager Approval requests. |
| `web/src/stores/use-manager-approval-store.test.ts` | Tests for Manager Approval store lifecycle. |
| `web/src/components/feedback/manager-approval-dialog.tsx` | Touch dialog collecting Manager login code and PIN. |
| `web/src/features/shift/utils/denomination.ts` | VND denomination definitions and arithmetic. |
| `web/src/features/shift/utils/denomination.test.ts` | Tests for denomination calculations. |
| `web/src/features/shift/utils/discrepancy.ts` | Discrepancy dimension & reason label mappings and derivation. |
| `web/src/features/shift/utils/discrepancy.test.ts` | Tests for discrepancy resolution logic. |
| `web/src/features/shift/api/use-shift.ts` | Feature API seam wrapping Orval shift hooks with `unwrap()`. |
| `web/src/features/shift/components/denomination-calculator.tsx` | Interactive table component for counting banknotes by denomination. |
| `web/src/features/shift/components/no-active-shift-view.tsx` | Empty state shown when no shift is currently open. |
| `web/src/features/shift/components/open-shift-dialog.tsx` | Dialog to open shift and set opening float amount. |
| `web/src/features/shift/components/open-shift-dashboard.tsx` | Main dashboard while shift is `OPEN` (metadata + action triggers). |
| `web/src/features/shift/components/cash-movement-dialog.tsx` | Dialog for Pay In / Pay Out with Manager Approval prompt. |
| `web/src/features/shift/components/closing-reconciliation-view.tsx` | Dashboard while shift is `CLOSING` (reconciliation snapshot & tables). |
| `web/src/features/shift/components/cash-count-dialog.tsx` | Dialog to record a cash recount. |
| `web/src/features/shift/components/qr-observation-dialog.tsx` | Dialog to record QR observed received & refunded amounts. |
| `web/src/features/shift/components/close-shift-dialog.tsx` | Dialog to resolve discrepancies and finalize shift closure. |

**Modified:**

| File | Change |
| :--- | :--- |
| `web/src/routes/_app.tsx` | Mount `<ManagerApprovalDialog />` so any feature can invoke it. |
| `web/src/features/shift/components/shift-view.tsx` | Replace placeholder with state-driven container router. |

---

### Task 1: Command Idempotency Helper (`newRequestId`)

**Files:**
- Create: `web/src/lib/command.ts`
- Create: `web/src/lib/command.test.ts`

**Interfaces:**
- Consumes: `crypto.randomUUID`
- Produces: `newRequestId(): string`, `withRequestId<T>(payload: T, requestId?: string): T & { request_id: string }`

- [ ] **Step 1: Write failing test for `newRequestId`**

Create `web/src/lib/command.test.ts`:
```ts
import { describe, expect, it } from "bun:test";
import { newRequestId, withRequestId } from "./command";

describe("newRequestId", () => {
  it("generates a valid UUID v4 string", () => {
    const id = newRequestId();
    expect(id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
    );
  });

  it("generates unique IDs across calls", () => {
    const id1 = newRequestId();
    const id2 = newRequestId();
    expect(id1).not.toBe(id2);
  });
});

describe("withRequestId", () => {
  it("attaches a new request_id if none provided", () => {
    const payload = { amount: 1000 };
    const wrapped = withRequestId(payload);
    expect(wrapped.amount).toBe(1000);
    expect(wrapped.request_id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
    );
  });

  it("reuses provided request_id if specified", () => {
    const fixedId = "11111111-1111-4111-8111-111111111111";
    const payload = { amount: 2000 };
    const wrapped = withRequestId(payload, fixedId);
    expect(wrapped.request_id).toBe(fixedId);
  });
});
```

- [ ] **Step 2: Run test to verify failure**

Run: `cd web && bun test src/lib/command.test.ts`
Expected: FAIL (Cannot find module `./command`)

- [ ] **Step 3: Implement `command.ts`**

Create `web/src/lib/command.ts`:
```ts
export function newRequestId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === "x" ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

export function withRequestId<T extends Record<string, unknown>>(
  payload: T,
  requestId?: string
): T & { request_id: string } {
  return {
    ...payload,
    request_id: requestId ?? newRequestId(),
  };
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `cd web && bun test src/lib/command.test.ts`
Expected: PASS (2 tests pass)

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/command.ts web/src/lib/command.test.ts
git commit -m "feat(web): add newRequestId and withRequestId command helpers"
```

---

### Task 2: Manager Approval Zustand Store & Dialog Component

**Files:**
- Create: `web/src/stores/use-manager-approval-store.ts`
- Create: `web/src/stores/use-manager-approval-store.test.ts`
- Create: `web/src/components/feedback/manager-approval-dialog.tsx`
- Modify: `web/src/routes/_app.tsx`

**Interfaces:**
- Consumes: `useSessionStore` (to check if current user is MANAGER and get default login code), `PinPad` from `features/auth/components/pin-pad`
- Produces: `useManagerApprovalStore`, `promptApproval(request)` returning `Promise<ManagerApprovalCredentials>`, `<ManagerApprovalDialog />`

- [ ] **Step 1: Write failing test for `useManagerApprovalStore`**

Create `web/src/stores/use-manager-approval-store.test.ts`:
```ts
import { describe, expect, it } from "bun:test";
import { useManagerApprovalStore } from "./use-manager-approval-store";

describe("useManagerApprovalStore", () => {
  it("starts in closed state with null request", () => {
    const state = useManagerApprovalStore.getState();
    expect(state.isOpen).toBe(false);
    expect(state.request).toBeNull();
  });

  it("opens modal and resolves promise on confirm", async () => {
    const store = useManagerApprovalStore.getState();
    const promptPromise = store.promptApproval({
      title: "Xác nhận duyệt",
      description: "Yêu cầu mã PIN Quản lý",
    });

    expect(useManagerApprovalStore.getState().isOpen).toBe(true);
    expect(useManagerApprovalStore.getState().request?.title).toBe("Xác nhận duyệt");

    useManagerApprovalStore.getState().confirm({
      approverLoginCode: "MGR01",
      managerPin: "1234",
    });

    const result = await promptPromise;
    expect(result.approverLoginCode).toBe("MGR01");
    expect(result.managerPin).toBe("1234");
    expect(useManagerApprovalStore.getState().isOpen).toBe(false);
  });

  it("rejects promise on cancel", async () => {
    const store = useManagerApprovalStore.getState();
    const promptPromise = store.promptApproval({
      title: "Huỷ thao tác",
      description: "Thao tác huỷ",
    });

    useManagerApprovalStore.getState().cancel();

    expect(promptPromise).rejects.toThrow("MANAGER_APPROVAL_CANCELLED");
    expect(useManagerApprovalStore.getState().isOpen).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify failure**

Run: `cd web && bun test src/stores/use-manager-approval-store.test.ts`
Expected: FAIL (Cannot find module)

- [ ] **Step 3: Implement `use-manager-approval-store.ts`**

Create `web/src/stores/use-manager-approval-store.ts`:
```ts
import { create } from "zustand";

export interface ManagerApprovalRequest {
  title: string;
  description: string;
  confirmLabel?: string;
}

export interface ManagerApprovalCredentials {
  approverLoginCode: string;
  managerPin: string;
}

export interface ManagerApprovalState {
  isOpen: boolean;
  request: ManagerApprovalRequest | null;
  resolve: ((creds: ManagerApprovalCredentials) => void) | null;
  reject: ((error: Error) => void) | null;
  promptApproval: (request: ManagerApprovalRequest) => Promise<ManagerApprovalCredentials>;
  confirm: (creds: ManagerApprovalCredentials) => void;
  cancel: () => void;
}

export const useManagerApprovalStore = create<ManagerApprovalState>((set, get) => ({
  isOpen: false,
  request: null,
  resolve: null,
  reject: null,

  promptApproval: (request) => {
    return new Promise<ManagerApprovalCredentials>((resolve, reject) => {
      set({
        isOpen: true,
        request,
        resolve,
        reject,
      });
    });
  },

  confirm: (creds) => {
    const { resolve } = get();
    if (resolve) resolve(creds);
    set({ isOpen: false, request: null, resolve: null, reject: null });
  },

  cancel: () => {
    const { reject } = get();
    if (reject) reject(new Error("MANAGER_APPROVAL_CANCELLED"));
    set({ isOpen: false, request: null, resolve: null, reject: null });
  },
}));
```

- [ ] **Step 4: Run test to verify pass**

Run: `cd web && bun test src/stores/use-manager-approval-store.test.ts`
Expected: PASS (3 tests pass)

- [ ] **Step 5: Implement `ManagerApprovalDialog` component**

Create `web/src/components/feedback/manager-approval-dialog.tsx`:
```tsx
import { useState, useEffect } from "react";
import { ShieldCheck, X } from "lucide-react";
import { useManagerApprovalStore } from "@/stores/use-manager-approval-store";
import { useSessionStore } from "@/stores/use-session-store";
import { PinPad } from "@/features/auth/components/pin-pad";
import { Input } from "@/components/ui/input";
import { playClick, playError, playAction } from "@/lib/sound";

export function ManagerApprovalDialog() {
  const { isOpen, request, confirm, cancel } = useManagerApprovalStore();
  const staff = useSessionStore((s) => s.staff);
  const isCurrentManager = staff?.roles?.includes("MANAGER") ?? false;

  const [loginCode, setLoginCode] = useState("");
  const [pin, setPin] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (isOpen) {
      setLoginCode(isCurrentManager && staff ? staff.login_code : "");
      setPin("");
      setError(null);
    }
  }, [isOpen, isCurrentManager, staff]);

  if (!isOpen || !request) return null;

  const handleDigit = (digit: string) => {
    if (pin.length < 8) {
      playClick();
      setPin((prev) => prev + digit);
      setError(null);
    }
  };

  const handleBackspace = () => {
    playClick();
    setPin((prev) => prev.slice(0, -1));
    setError(null);
  };

  const handleClear = () => {
    playClick();
    setPin("");
    setError(null);
  };

  const handleConfirm = () => {
    const trimmedCode = loginCode.trim();
    if (!trimmedCode) {
      playError();
      setError("Vui lòng nhập mã nhân viên Quản lý");
      return;
    }
    if (pin.length < 4) {
      playError();
      setError("Mã PIN Quản lý phải từ 4 đến 8 chữ số");
      return;
    }

    playAction();
    confirm({
      approverLoginCode: trimmedCode,
      managerPin: pin,
    });
  };

  const handleClose = () => {
    playClick();
    cancel();
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-sm overflow-hidden flex flex-col p-6 gap-4">
        {/* Header */}
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-xl bg-primary/10 text-primary flex items-center justify-center">
              <ShieldCheck className="w-5 h-5" />
            </div>
            <div>
              <h3 className="text-base font-bold text-foreground">{request.title}</h3>
              <p className="text-xs text-muted-foreground">{request.description}</p>
            </div>
          </div>
          <button
            type="button"
            onClick={handleClose}
            className="p-1 rounded-lg text-muted-foreground hover:bg-muted transition"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Login Code Input */}
        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Mã đăng nhập Quản lý (Login Code)
          </label>
          <Input
            value={loginCode}
            onChange={(e) => setLoginCode(e.target.value.toUpperCase())}
            placeholder="VD: MGR01"
            className="font-mono uppercase tracking-wider"
            disabled={isCurrentManager}
          />
        </div>

        {/* PIN Display */}
        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Mã PIN Quản lý (4-8 số)
          </label>
          <div className="h-12 border border-input rounded-xl bg-background flex items-center justify-center font-mono text-2xl tracking-widest text-foreground select-none">
            {pin ? "•".repeat(pin.length) : <span className="text-muted-foreground text-sm font-sans">Nhập mã PIN</span>}
          </div>
        </div>

        {error && (
          <p className="text-xs text-destructive text-center font-medium">{error}</p>
        )}

        {/* Touch PinPad */}
        <PinPad
          onDigit={handleDigit}
          onBackspace={handleBackspace}
          onClear={handleClear}
          onSubmit={handleConfirm}
          disabled={pin.length < 4}
          submitLabel={request.confirmLabel ?? "Xác nhận duyệt"}
        />
      </div>
    </div>
  );
}
```

- [ ] **Step 6: Mount `ManagerApprovalDialog` in `_app.tsx`**

Modify `web/src/routes/_app.tsx`:
Add `import { ManagerApprovalDialog } from "@/components/feedback/manager-approval-dialog";`
Render `<ManagerApprovalDialog />` beside `<LockOverlay />`.

- [ ] **Step 7: Commit**

```bash
git add web/src/stores/use-manager-approval-store.ts web/src/stores/use-manager-approval-store.test.ts web/src/components/feedback/manager-approval-dialog.tsx web/src/routes/_app.tsx
git commit -m "feat(web): add Manager Approval store and touch dialog"
```

---

### Task 3: Denomination & Discrepancy Utilities

**Files:**
- Create: `web/src/features/shift/utils/denomination.ts`
- Create: `web/src/features/shift/utils/denomination.test.ts`
- Create: `web/src/features/shift/utils/discrepancy.ts`
- Create: `web/src/features/shift/utils/discrepancy.test.ts`

**Interfaces:**
- Produces:
  - `VND_DENOMINATIONS`, `calculateDenominationTotal(counts: Record<number, number>): number`
  - `formatDenomination(denom: number): string`
  - `DIMENSION_LABELS`, `REASON_LABELS`, `buildDiscrepanciesList(...)`

- [ ] **Step 1: Write failing tests for `denomination.ts`**

Create `web/src/features/shift/utils/denomination.test.ts`:
```ts
import { describe, expect, it } from "bun:test";
import {
  VND_DENOMINATIONS,
  calculateDenominationTotal,
  formatDenomination,
} from "./denomination";

describe("denomination utilities", () => {
  it("lists all 9 Vietnamese Dong denominations", () => {
    expect(VND_DENOMINATIONS).toEqual([
      500_000, 200_000, 100_000, 50_000, 20_000, 10_000, 5_000, 2_000, 1_000,
    ]);
  });

  it("calculates total from count mapping correctly", () => {
    const counts = {
      500_000: 2, // 1,000,000
      100_000: 3, // 300,000
      50_000: 1,  // 50,000
      1_000: 5,   // 5,000
    };
    expect(calculateDenominationTotal(counts)).toBe(1_355_000);
  });

  it("ignores negative or null counts", () => {
    const counts = {
      500_000: -1,
      200_000: 1,
    };
    expect(calculateDenominationTotal(counts)).toBe(200_000);
  });

  it("formats denominations cleanly", () => {
    expect(formatDenomination(500_000)).toBe("500.000 đ");
    expect(formatDenomination(20_000)).toBe("20.000 đ");
  });
});
```

- [ ] **Step 2: Run test to verify failure**

Run: `cd web && bun test src/features/shift/utils/denomination.test.ts`
Expected: FAIL

- [ ] **Step 3: Implement `denomination.ts`**

Create `web/src/features/shift/utils/denomination.ts`:
```ts
export const VND_DENOMINATIONS = [
  500_000, 200_000, 100_000, 50_000, 20_000, 10_000, 5_000, 2_000, 1_000,
] as const;

export type DenominationCounts = Record<number, number>;

export function calculateDenominationTotal(counts: DenominationCounts): number {
  return Object.entries(counts).reduce((sum, [denom, count]) => {
    const validCount = Math.max(0, count || 0);
    return sum + Number(denom) * validCount;
  }, 0);
}

export function formatDenomination(denom: number): string {
  return `${denom.toLocaleString("vi-VN")} đ`;
}
```

- [ ] **Step 4: Write failing tests for `discrepancy.ts`**

Create `web/src/features/shift/utils/discrepancy.test.ts`:
```ts
import { describe, expect, it } from "bun:test";
import {
  DIMENSION_LABELS,
  REASON_LABELS,
  deriveNonZeroDimensions,
} from "./discrepancy";

describe("discrepancy utilities", () => {
  it("translates dimensions to Vietnamese", () => {
    expect(DIMENSION_LABELS.CASH).toBe("Tiền mặt");
    expect(DIMENSION_LABELS.MANUAL_QR_RECEIVED).toBe("VietQR Đã Nhận");
    expect(DIMENSION_LABELS.MANUAL_QR_REFUNDED).toBe("VietQR Hoàn Tiền");
  });

  it("translates reasons to Vietnamese", () => {
    expect(REASON_LABELS.CASH_COUNT_DIFFERENCE).toBe("Chênh lệch tiền mặt kiểm đếm");
    expect(REASON_LABELS.QR_OBSERVATION_DIFFERENCE).toBe("Chênh lệch đối soát VietQR");
    expect(REASON_LABELS.UNEXPLAINED).toBe("Chưa rõ nguyên nhân");
  });

  it("derives only dimensions with nonzero difference", () => {
    const dimensions = [
      { dimension: "CASH" as const, expected_vnd: 1000, observed_vnd: 900, difference_vnd: -100, recheck_required: false },
      { dimension: "MANUAL_QR_RECEIVED" as const, expected_vnd: 500, observed_vnd: 500, difference_vnd: 0, recheck_required: false },
      { dimension: "MANUAL_QR_REFUNDED" as const, expected_vnd: 0, observed_vnd: 50, difference_vnd: 50, recheck_required: false },
    ];
    const nonZero = deriveNonZeroDimensions(dimensions);
    expect(nonZero.map((d) => d.dimension)).toEqual(["CASH", "MANUAL_QR_REFUNDED"]);
  });
});
```

- [ ] **Step 5: Implement `discrepancy.ts`**

Create `web/src/features/shift/utils/discrepancy.ts`:
```ts
import type {
  ShiftCloseDiscrepancyInputDimension,
  ShiftCloseDiscrepancyInputReason,
  ShiftReconciliationPreviewEntry,
} from "@/api/generated/models";

export const DIMENSION_LABELS: Record<ShiftCloseDiscrepancyInputDimension, string> = {
  CASH: "Tiền mặt",
  MANUAL_QR_RECEIVED: "VietQR Đã Nhận",
  MANUAL_QR_REFUNDED: "VietQR Hoàn Tiền",
};

export const REASON_LABELS: Record<ShiftCloseDiscrepancyInputReason, string> = {
  CASH_COUNT_DIFFERENCE: "Chênh lệch tiền mặt kiểm đếm",
  QR_OBSERVATION_DIFFERENCE: "Chênh lệch đối soát VietQR",
  UNEXPLAINED: "Chưa rõ nguyên nhân",
};

export function deriveNonZeroDimensions(
  dimensions: ShiftReconciliationPreviewEntry[]
): ShiftReconciliationPreviewEntry[] {
  return dimensions.filter(
    (d) => d.difference_vnd !== undefined && d.difference_vnd !== null && d.difference_vnd !== 0
  );
}
```

- [ ] **Step 6: Run tests to verify pass**

Run: `cd web && bun test src/features/shift/utils/`
Expected: PASS (all tests in denomination and discrepancy pass)

- [ ] **Step 7: Commit**

```bash
git add web/src/features/shift/utils/
git commit -m "feat(web): add shift denomination and discrepancy calculation utilities"
```

---

### Task 4: Shift API Seam (`features/shift/api/use-shift.ts`)

**Files:**
- Create: `web/src/features/shift/api/use-shift.ts`

**Interfaces:**
- Consumes: generated hooks from `@/api/generated/endpoints/shifts/shifts`, `unwrap` from `@/lib/unwrap`, `queryClient` from `@/lib/query-client`
- Produces:
  - `useCurrentShift()`
  - `useOpenShift()`
  - `useRecordCashMovement()`
  - `useStartReconciliation()`
  - `useRecordCashCount()`
  - `useRecordQRObservation()`
  - `useCloseShift()`

- [ ] **Step 1: Implement `use-shift.ts`**

Create `web/src/features/shift/api/use-shift.ts`:
```ts
import { useQueryClient } from "@tanstack/react-query";
import {
  useGetShiftsCurrent,
  usePostShifts,
  usePostShiftsShiftIdCashMovements,
  usePostShiftsShiftIdReconciliation,
  usePostShiftsShiftIdReconciliationCashCounts,
  usePostShiftsShiftIdReconciliationQrObservations,
  usePostShiftsShiftIdClose,
  getGetShiftsCurrentQueryKey,
} from "@/api/generated/endpoints/shifts/shifts";
import { unwrap } from "@/lib/unwrap";
import type {
  ShiftOpenShiftCommand,
  ShiftRecordCashMovementCommand,
  ShiftStartReconciliationCommand,
  ShiftRecordCashCountCommand,
  ShiftRecordQRObservationCommand,
  ShiftCloseShiftCommand,
} from "@/api/generated/models";

export function useCurrentShift() {
  return useGetShiftsCurrent({
    query: {
      select: unwrap,
      staleTime: 15_000,
    },
  });
}

export function useOpenShift() {
  const queryClient = useQueryClient();
  const mutation = usePostShifts({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetShiftsCurrentQueryKey() });
      },
    },
  });

  return {
    ...mutation,
    openShift: async (command: ShiftOpenShiftCommand) => {
      const res = await mutation.mutateAsync({ data: command });
      return unwrap(res);
    },
  };
}

export function useRecordCashMovement(shiftId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostShiftsShiftIdCashMovements({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetShiftsCurrentQueryKey() });
      },
    },
  });

  return {
    ...mutation,
    recordCashMovement: async (command: ShiftRecordCashMovementCommand) => {
      const res = await mutation.mutateAsync({ shiftId, data: command });
      return unwrap(res);
    },
  };
}

export function useStartReconciliation(shiftId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostShiftsShiftIdReconciliation({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetShiftsCurrentQueryKey() });
      },
    },
  });

  return {
    ...mutation,
    startReconciliation: async (command: ShiftStartReconciliationCommand) => {
      const res = await mutation.mutateAsync({ shiftId, data: command });
      return unwrap(res);
    },
  };
}

export function useRecordCashCount(shiftId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostShiftsShiftIdReconciliationCashCounts({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetShiftsCurrentQueryKey() });
      },
    },
  });

  return {
    ...mutation,
    recordCashCount: async (command: ShiftRecordCashCountCommand) => {
      const res = await mutation.mutateAsync({ shiftId, data: command });
      return unwrap(res);
    },
  };
}

export function useRecordQRObservation(shiftId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostShiftsShiftIdReconciliationQrObservations({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetShiftsCurrentQueryKey() });
      },
    },
  });

  return {
    ...mutation,
    recordQRObservation: async (command: ShiftRecordQRObservationCommand) => {
      const res = await mutation.mutateAsync({ shiftId, data: command });
      return unwrap(res);
    },
  };
}

export function useCloseShift(shiftId: string) {
  const queryClient = useQueryClient();
  const mutation = usePostShiftsShiftIdClose({
    mutation: {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getGetShiftsCurrentQueryKey() });
      },
    },
  });

  return {
    ...mutation,
    closeShift: async (command: ShiftCloseShiftCommand) => {
      const res = await mutation.mutateAsync({ shiftId, data: command });
      return unwrap(res);
    },
  };
}
```

- [ ] **Step 2: Typecheck**

Run: `cd web && bun run build` (or `bun run lint`)
Expected: PASS with 0 errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/features/shift/api/use-shift.ts
git commit -m "feat(web): add use-shift api seam wrapping generated shift endpoints"
```

---

### Task 5: Denomination Calculator Component

**Files:**
- Create: `web/src/features/shift/components/denomination-calculator.tsx`

**Interfaces:**
- Consumes: `VND_DENOMINATIONS`, `calculateDenominationTotal`, `formatDenomination` from `@/features/shift/utils/denomination`
- Produces: `<DenominationCalculator counts={counts} onChange={setCounts} />`

- [ ] **Step 1: Implement `denomination-calculator.tsx`**

Create `web/src/features/shift/components/denomination-calculator.tsx`:
```tsx
import { Plus, Minus } from "lucide-react";
import {
  VND_DENOMINATIONS,
  type DenominationCounts,
  formatDenomination,
} from "@/features/shift/utils/denomination";
import { formatVND } from "@/lib/utils";
import { playClick } from "@/lib/sound";

interface DenominationCalculatorProps {
  counts: DenominationCounts;
  onChange: (counts: DenominationCounts) => void;
}

export function DenominationCalculator({ counts, onChange }: DenominationCalculatorProps) {
  const handleDelta = (denom: number, delta: number) => {
    playClick();
    const current = counts[denom] || 0;
    const next = Math.max(0, current + delta);
    onChange({ ...counts, [denom]: next });
  };

  const handleInput = (denom: number, raw: string) => {
    const val = parseInt(raw, 10);
    const next = isNaN(val) ? 0 : Math.max(0, val);
    onChange({ ...counts, [denom]: next });
  };

  return (
    <div className="border border-border rounded-xl bg-card overflow-hidden">
      <div className="p-3 bg-muted/40 border-b border-border flex items-center justify-between text-xs font-semibold text-muted-foreground">
        <span>Mệnh giá</span>
        <span className="text-center">Số lượng tờ</span>
        <span className="text-right">Thành tiền</span>
      </div>
      <div className="divide-y divide-border/60 max-h-64 overflow-y-auto">
        {VND_DENOMINATIONS.map((denom) => {
          const qty = counts[denom] || 0;
          const subtotal = denom * qty;
          return (
            <div key={denom} className="px-3 py-2 flex items-center justify-between gap-2 text-sm">
              <span className="font-mono font-medium text-foreground w-28">
                {formatDenomination(denom)}
              </span>

              {/* Quantity Controls */}
              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  onClick={() => handleDelta(denom, -1)}
                  disabled={qty <= 0}
                  className="w-8 h-8 rounded-lg border border-input bg-background hover:bg-muted disabled:opacity-40 flex items-center justify-center text-foreground transition active:scale-95"
                >
                  <Minus className="w-3.5 h-3.5" />
                </button>
                <input
                  type="text"
                  inputMode="numeric"
                  value={qty === 0 ? "" : qty}
                  placeholder="0"
                  onChange={(e) => handleInput(denom, e.target.value)}
                  className="w-12 h-8 text-center font-mono font-semibold border border-input rounded-lg bg-background text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
                />
                <button
                  type="button"
                  onClick={() => handleDelta(denom, 1)}
                  className="w-8 h-8 rounded-lg border border-input bg-background hover:bg-muted flex items-center justify-center text-foreground transition active:scale-95"
                >
                  <Plus className="w-3.5 h-3.5" />
                </button>
              </div>

              {/* Subtotal */}
              <span className="font-mono text-right font-medium text-muted-foreground w-28">
                {formatVND(subtotal)}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/features/shift/components/denomination-calculator.tsx
git commit -m "feat(web): add interactive denomination calculator table"
```

---

### Task 6: No Active Shift View & Open Shift Dialog

**Files:**
- Create: `web/src/features/shift/components/no-active-shift-view.tsx`
- Create: `web/src/features/shift/components/open-shift-dialog.tsx`

**Interfaces:**
- Consumes: `useOpenShift`, `newRequestId`, `DenominationCalculator`, `calculateDenominationTotal`
- Produces: `<NoActiveShiftView onOpen={() => setIsOpen(true)} />`, `<OpenShiftDialog isOpen={isOpen} onClose={() => setIsOpen(false)} />`

- [ ] **Step 1: Implement `no-active-shift-view.tsx`**

Create `web/src/features/shift/components/no-active-shift-view.tsx`:
```tsx
import { Clock, Play, WalletCards } from "lucide-react";
import { Button } from "@/components/ui/button";

interface NoActiveShiftViewProps {
  onOpen: () => void;
}

export function NoActiveShiftView({ onOpen }: NoActiveShiftViewProps) {
  return (
    <div className="flex flex-col items-center justify-center p-8 text-center max-w-lg mx-auto min-h-[60vh] gap-6 animate-in fade-in duration-300">
      <div className="w-20 h-20 rounded-3xl bg-primary/10 text-primary flex items-center justify-center shadow-inner">
        <Clock className="w-10 h-10 stroke-[1.75]" />
      </div>

      <div className="space-y-2">
        <h2 className="text-xl font-bold text-foreground">Chưa có ca làm việc nào đang mở</h2>
        <p className="text-sm text-muted-foreground leading-relaxed">
          Quầy thu ngân cần mở ca làm việc và ghi nhận số tiền mặt đầu ca (Opening Float) trước khi có thể thực hiện thanh toán đơn hàng.
        </p>
      </div>

      <div className="p-4 rounded-xl border border-border bg-card/60 flex items-center gap-3 text-left w-full">
        <WalletCards className="w-5 h-5 text-primary shrink-0" />
        <div className="text-xs text-muted-foreground">
          <span className="font-semibold text-foreground block">Nguyên tắc kiểm đếm mù:</span>
          Số tiền đầu ca sẽ được lưu an toàn trên máy chủ để đối soát kết ca.
        </div>
      </div>

      <Button
        type="button"
        onClick={onOpen}
        className="min-h-12 h-12 px-8 rounded-xl text-base font-bold shadow-md hover:shadow-lg transition active:scale-98"
      >
        <Play className="w-5 h-5 fill-current mr-2" />
        Mở ca làm việc ngay
      </Button>
    </div>
  );
}
```

- [ ] **Step 2: Implement `open-shift-dialog.tsx`**

Create `web/src/features/shift/components/open-shift-dialog.tsx`:
```tsx
import { useState } from "react";
import { X, Play, Calculator } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { DenominationCalculator } from "./denomination-calculator";
import {
  type DenominationCounts,
  calculateDenominationTotal,
} from "@/features/shift/utils/denomination";
import { useOpenShift } from "@/features/shift/api/use-shift";
import { newRequestId } from "@/lib/command";
import { formatVND } from "@/lib/utils";
import { playClick, playSuccess, playError } from "@/lib/sound";

interface OpenShiftDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

export function OpenShiftDialog({ isOpen, onClose }: OpenShiftDialogProps) {
  const { openShift, isPending } = useOpenShift();
  const [showCalculator, setShowCalculator] = useState(false);
  const [directAmount, setDirectAmount] = useState<string>("1000000");
  const [counts, setCounts] = useState<DenominationCounts>({
    500_000: 1,
    200_000: 2,
    100_000: 1,
  });
  const [error, setError] = useState<string | null>(null);

  if (!isOpen) return null;

  const currentTotal = showCalculator
    ? calculateDenominationTotal(counts)
    : parseInt(directAmount.replace(/\D/g, ""), 10) || 0;

  const handleOpen = async () => {
    playClick();
    if (currentTotal < 0) {
      playError();
      setError("Số tiền đầu ca không được âm");
      return;
    }

    try {
      await openShift({
        request_id: newRequestId(),
        opening_float_vnd: currentTotal,
      });
      playSuccess();
      onClose();
    } catch (err: unknown) {
      playError();
      setError(err instanceof Error ? err.message : "Không thể mở ca làm việc");
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-lg overflow-hidden flex flex-col p-6 gap-4">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-base font-bold text-foreground">Mở ca làm việc (Opening Shift)</h3>
            <p className="text-xs text-muted-foreground">Thiết lập số tiền mặt có sẵn trong két đầu ca</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg text-muted-foreground hover:bg-muted transition"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Mode Toggle */}
        <div className="flex items-center justify-between">
          <span className="text-xs font-semibold text-muted-foreground">Cách nhập số tiền:</span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => setShowCalculator(!showCalculator)}
            className="h-8 text-xs font-medium"
          >
            <Calculator className="w-3.5 h-3.5 mr-1" />
            {showCalculator ? "Nhập số tiền trực tiếp" : "Bảng kê mệnh giá"}
          </Button>
        </div>

        {/* Input area */}
        {showCalculator ? (
          <DenominationCalculator counts={counts} onChange={setCounts} />
        ) : (
          <div className="space-y-1.5">
            <label className="text-xs font-semibold text-muted-foreground">Số tiền đầu ca (VND)</label>
            <Input
              type="text"
              inputMode="numeric"
              value={directAmount}
              onChange={(e) => setDirectAmount(e.target.value)}
              className="font-mono text-xl font-bold h-12"
              placeholder="1000000"
            />
          </div>
        )}

        {/* Total Summary */}
        <div className="p-3.5 rounded-xl bg-primary/10 border border-primary/20 flex items-center justify-between">
          <span className="text-xs font-semibold text-primary">Tổng tiền mặt bàn giao:</span>
          <span className="font-mono text-lg font-bold text-primary">{formatVND(currentTotal)}</span>
        </div>

        {error && <p className="text-xs text-destructive text-center font-medium">{error}</p>}

        {/* Action buttons */}
        <div className="flex items-center gap-3 mt-2">
          <Button type="button" variant="outline" onClick={onClose} className="w-1/2 h-12 rounded-xl">
            Huỷ
          </Button>
          <Button
            type="button"
            onClick={handleOpen}
            disabled={isPending}
            className="w-1/2 h-12 rounded-xl font-bold shadow-sm"
          >
            <Play className="w-4 h-4 fill-current mr-1.5" />
            {isPending ? "Đang mở..." : "Xác nhận mở ca"}
          </Button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Commit**

```bash
git add web/src/features/shift/components/no-active-shift-view.tsx web/src/features/shift/components/open-shift-dialog.tsx
git commit -m "feat(web): add NoActiveShiftView and OpenShiftDialog components"
```

---

### Task 7: Open Shift Dashboard & Cash Movement Dialog

**Files:**
- Create: `web/src/features/shift/components/open-shift-dashboard.tsx`
- Create: `web/src/features/shift/components/cash-movement-dialog.tsx`

**Interfaces:**
- Consumes: `useRecordCashMovement`, `useStartReconciliation`, `useManagerApprovalStore`, `newRequestId`
- Produces: `<OpenShiftDashboard shift={shift} />`, `<CashMovementDialog />`

- [ ] **Step 1: Implement `cash-movement-dialog.tsx`**

Create `web/src/features/shift/components/cash-movement-dialog.tsx`:
```tsx
import { useState } from "react";
import { X, ArrowDownRight, ArrowUpRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useRecordCashMovement } from "@/features/shift/api/use-shift";
import { useManagerApprovalStore } from "@/stores/use-manager-approval-store";
import { newRequestId } from "@/lib/command";
import { formatVND } from "@/lib/utils";
import { playClick, playSuccess, playError } from "@/lib/sound";
import type { ShiftRecordCashMovementCommandReason } from "@/api/generated/models";

interface CashMovementDialogProps {
  shiftId: string;
  isOpen: boolean;
  onClose: () => void;
  defaultMethod?: "PAY_IN" | "PAY_OUT";
}

const REASONS: { value: ShiftRecordCashMovementCommandReason; label: string }[] = [
  { value: "ADD_CHANGE_FUND", label: "Bổ sung tiền thối lẻ (Change Fund)" },
  { value: "REMOVE_EXCESS_FLOAT", label: "Rút bớt tiền mặt dư thừa" },
  { value: "SAFE_DROP", label: "Chuyển tiền vào két an toàn (Safe Drop)" },
  { value: "OTHER", label: "Lý do khác (Bắt buộc ghi chú)" },
];

export function CashMovementDialog({
  shiftId,
  isOpen,
  onClose,
  defaultMethod = "PAY_IN",
}: CashMovementDialogProps) {
  const { recordCashMovement, isPending } = useRecordCashMovement(shiftId);
  const promptApproval = useManagerApprovalStore((s) => s.promptApproval);

  const [method, setMethod] = useState<"PAY_IN" | "PAY_OUT">(defaultMethod);
  const [amount, setAmount] = useState<string>("100000");
  const [reason, setReason] = useState<ShiftRecordCashMovementCommandReason>("ADD_CHANGE_FUND");
  const [note, setNote] = useState<string>("");
  const [error, setError] = useState<string | null>(null);

  if (!isOpen) return null;

  const numAmount = parseInt(amount.replace(/\D/g, ""), 10) || 0;

  const handleSubmit = async () => {
    playClick();
    if (numAmount <= 0) {
      playError();
      setError("Số tiền phải lớn hơn 0");
      return;
    }
    if (reason === "OTHER" && !note.trim()) {
      playError();
      setError("Lý do khác bắt buộc phải nhập ghi chú");
      return;
    }

    try {
      // Prompt Manager Approval
      const creds = await promptApproval({
        title: method === "PAY_IN" ? "Duyệt Nộp Quỹ Tiền Mặt" : "Duyệt Rút Quỹ Tiền Mặt",
        description: `Xác nhận ${method === "PAY_IN" ? "nộp thêm" : "rút chi"} ${formatVND(numAmount)} tiền mặt.`,
        confirmLabel: "Phê duyệt biến động quỹ",
      });

      await recordCashMovement({
        request_id: newRequestId(),
        method,
        amount_vnd: numAmount,
        reason,
        note: note.trim() ? note.trim() : undefined,
        approver_login_code: creds.approverLoginCode,
        manager_pin: creds.managerPin,
      });

      playSuccess();
      onClose();
    } catch (err: unknown) {
      playError();
      if (err instanceof Error && err.message === "MANAGER_APPROVAL_CANCELLED") {
        return;
      }
      setError(err instanceof Error ? err.message : "Lỗi ghi nhận biến động tiền mặt");
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-md overflow-hidden flex flex-col p-6 gap-4">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-base font-bold text-foreground">Giao dịch tiền mặt (Pay In / Out)</h3>
            <p className="text-xs text-muted-foreground">Yêu cầu Quản lý phê duyệt thao tác</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg text-muted-foreground hover:bg-muted transition"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Method Toggle */}
        <div className="grid grid-cols-2 gap-2 p-1 rounded-xl bg-muted">
          <button
            type="button"
            onClick={() => setMethod("PAY_IN")}
            className={`min-h-10 h-10 rounded-lg text-xs font-bold flex items-center justify-center gap-1.5 transition ${
              method === "PAY_IN" ? "bg-background text-emerald-600 shadow-xs" : "text-muted-foreground"
            }`}
          >
            <ArrowDownRight className="w-4 h-4" />
            Nộp tiền (Pay In)
          </button>
          <button
            type="button"
            onClick={() => setMethod("PAY_OUT")}
            className={`min-h-10 h-10 rounded-lg text-xs font-bold flex items-center justify-center gap-1.5 transition ${
              method === "PAY_OUT" ? "bg-background text-rose-600 shadow-xs" : "text-muted-foreground"
            }`}
          >
            <ArrowUpRight className="w-4 h-4" />
            Rút tiền (Pay Out)
          </button>
        </div>

        {/* Amount */}
        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">Số tiền (VND)</label>
          <Input
            type="text"
            inputMode="numeric"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            className="font-mono text-xl font-bold h-12"
          />
          <div className="flex gap-1.5">
            {[50_000, 100_000, 200_000, 500_000].map((quick) => (
              <button
                key={quick}
                type="button"
                onClick={() => setAmount(quick.toString())}
                className="px-2 py-1 rounded-md text-[11px] font-mono font-medium border border-border bg-muted/40 hover:bg-muted transition"
              >
                +{quick / 1000}k
              </button>
            ))}
          </div>
        </div>

        {/* Reason Select */}
        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">Lý do giao dịch</label>
          <select
            value={reason}
            onChange={(e) => setReason(e.target.value as ShiftRecordCashMovementCommandReason)}
            className="w-full h-11 px-3 rounded-xl border border-input bg-background text-sm font-medium text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
          >
            {REASONS.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label}
              </option>
            ))}
          </select>
        </div>

        {/* Note */}
        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Ghi chú {reason === "OTHER" ? "(Bắt buộc)" : "(Tuỳ chọn)"}
          </label>
          <Input
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="Nhập lý do chi tiết..."
          />
        </div>

        {error && <p className="text-xs text-destructive text-center font-medium">{error}</p>}

        {/* Submit */}
        <div className="flex items-center gap-3 mt-2">
          <Button type="button" variant="outline" onClick={onClose} className="w-1/2 h-12 rounded-xl">
            Huỷ
          </Button>
          <Button
            type="button"
            onClick={handleSubmit}
            disabled={isPending}
            className="w-1/2 h-12 rounded-xl font-bold shadow-sm"
          >
            {isPending ? "Đang ghi..." : "Yêu cầu Quản lý duyệt"}
          </Button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Implement `open-shift-dashboard.tsx`**

Create `web/src/features/shift/components/open-shift-dashboard.tsx`:
```tsx
import { useState } from "react";
import { Clock, User, ArrowDownRight, ArrowUpRight, Lock, EyeOff, ShieldAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { CashMovementDialog } from "./cash-movement-dialog";
import { DenominationCalculator } from "./denomination-calculator";
import {
  type DenominationCounts,
  calculateDenominationTotal,
} from "@/features/shift/utils/denomination";
import { useStartReconciliation } from "@/features/shift/api/use-shift";
import { newRequestId } from "@/lib/command";
import { formatVND } from "@/lib/utils";
import { playClick, playSuccess, playError } from "@/lib/sound";
import type { ShiftCurrentShiftResponse } from "@/api/generated/models";

interface OpenShiftDashboardProps {
  shift: ShiftCurrentShiftResponse;
}

export function OpenShiftDashboard({ shift }: OpenShiftDashboardProps) {
  const { startReconciliation, isPending } = useStartReconciliation(shift.id);

  const [cashMovementOpen, setCashMovementOpen] = useState(false);
  const [movementMethod, setMovementMethod] = useState<"PAY_IN" | "PAY_OUT">("PAY_IN");
  const [showReconcileDialog, setShowReconcileDialog] = useState(false);

  const [counts, setCounts] = useState<DenominationCounts>({});
  const [reconcileError, setReconcileError] = useState<string | null>(null);

  const countedCash = calculateDenominationTotal(counts);

  const handleStartReconciliation = async () => {
    playClick();
    if (countedCash < 0) {
      playError();
      setReconcileError("Số tiền kiểm đếm không hợp lệ");
      return;
    }

    try {
      await startReconciliation({
        request_id: newRequestId(),
        counted_cash_vnd: countedCash,
      });
      playSuccess();
      setShowReconcileDialog(false);
    } catch (err: unknown) {
      playError();
      setReconcileError(err instanceof Error ? err.message : "Lỗi bắt đầu đối soát");
    }
  };

  const openCashMovement = (method: "PAY_IN" | "PAY_OUT") => {
    setMovementMethod(method);
    setCashMovementOpen(true);
  };

  const formattedOpenedAt = shift.opened_at
    ? new Date(shift.opened_at).toLocaleTimeString("vi-VN", {
        hour: "2-digit",
        minute: "2-digit",
        day: "2-digit",
        month: "2-digit",
      })
    : "—";

  return (
    <div className="space-y-6 max-w-5xl mx-auto p-4 sm:p-6 animate-in fade-in duration-200">
      {/* Top Banner Card */}
      <div className="p-6 rounded-2xl border border-border bg-card shadow-xs flex flex-col md:flex-row md:items-center justify-between gap-6">
        <div className="space-y-3">
          <div className="flex items-center gap-2.5">
            <Badge variant="default" className="bg-emerald-600 text-white font-bold px-3 py-1 text-xs">
              Ca đang hoạt động (OPEN)
            </Badge>
            <span className="text-xs font-mono text-muted-foreground">ID: {shift.id.slice(0, 8)}</span>
          </div>

          <div className="flex flex-wrap items-center gap-4 text-xs sm:text-sm text-muted-foreground">
            <div className="flex items-center gap-1.5">
              <User className="w-4 h-4 text-primary" />
              <span>Người mở ca: <strong className="text-foreground font-semibold">{shift.opener.display_name}</strong></span>
            </div>
            <div className="flex items-center gap-1.5">
              <Clock className="w-4 h-4 text-primary" />
              <span>Bắt đầu lúc: <strong className="text-foreground font-semibold">{formattedOpenedAt}</strong></span>
            </div>
          </div>
        </div>

        {/* Quick Action Buttons */}
        <div className="flex flex-wrap items-center gap-3">
          <Button
            type="button"
            variant="outline"
            onClick={() => openCashMovement("PAY_IN")}
            className="min-h-12 h-12 rounded-xl text-xs font-bold border-emerald-200 bg-emerald-50/60 hover:bg-emerald-100 text-emerald-800"
          >
            <ArrowDownRight className="w-4 h-4 mr-1 text-emerald-600" />
            Nộp quỹ (Pay In)
          </Button>

          <Button
            type="button"
            variant="outline"
            onClick={() => openCashMovement("PAY_OUT")}
            className="min-h-12 h-12 rounded-xl text-xs font-bold border-rose-200 bg-rose-50/60 hover:bg-rose-100 text-rose-800"
          >
            <ArrowUpRight className="w-4 h-4 mr-1 text-rose-600" />
            Rút quỹ (Pay Out)
          </Button>

          <Button
            type="button"
            onClick={() => setShowReconcileDialog(true)}
            className="min-h-12 h-12 rounded-xl text-xs font-bold shadow-sm"
          >
            <Lock className="w-4 h-4 mr-1.5" />
            Kiểm tiền & Kết ca
          </Button>
        </div>
      </div>

      {/* Blind Count Boundary Notification */}
      <div className="p-5 rounded-2xl border border-border bg-muted/40 flex items-start gap-4">
        <div className="w-10 h-10 rounded-xl bg-muted text-muted-foreground flex items-center justify-center shrink-0">
          <EyeOff className="w-5 h-5" />
        </div>
        <div className="space-y-1 text-xs sm:text-sm">
          <h4 className="font-bold text-foreground">Nguyên tắc kiểm đếm mù (Blind Count Boundary)</h4>
          <p className="text-muted-foreground leading-relaxed">
            Trong suốt ca bán hàng, hệ thống không hiển thị số tiền mặt kỳ vọng hay doanh thu để đảm bảo tính minh bạch và trung thực. Khi bấm <strong>"Kiểm tiền & Kết ca"</strong>, bạn sẽ nhập số tiền mặt thực tế đang có trong két trước khi hệ thống đối soát.
          </p>
        </div>
      </div>

      {/* Cash Movement Dialog */}
      <CashMovementDialog
        shiftId={shift.id}
        isOpen={cashMovementOpen}
        onClose={() => setCashMovementOpen(false)}
        defaultMethod={movementMethod}
      />

      {/* Reconcile Prompt Modal */}
      {showReconcileDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
          <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-lg overflow-hidden flex flex-col p-6 gap-4">
            <div>
              <h3 className="text-base font-bold text-foreground">Bắt đầu kiểm tiền đối soát (Reconciliation)</h3>
              <p className="text-xs text-muted-foreground">
                Kiểm đếm toàn bộ số tiền mặt đang có trong ngăn kéo thu ngân
              </p>
            </div>

            <DenominationCalculator counts={counts} onChange={setCounts} />

            <div className="p-3 rounded-xl bg-primary/10 border border-primary/20 flex items-center justify-between">
              <span className="text-xs font-semibold text-primary">Tổng tiền kiểm đếm:</span>
              <span className="font-mono text-lg font-bold text-primary">{formatVND(countedCash)}</span>
            </div>

            {reconcileError && (
              <p className="text-xs text-destructive text-center font-medium">{reconcileError}</p>
            )}

            <div className="flex items-center gap-3 mt-2">
              <Button
                type="button"
                variant="outline"
                onClick={() => setShowReconcileDialog(false)}
                className="w-1/2 h-12 rounded-xl"
              >
                Huỷ
              </Button>
              <Button
                type="button"
                onClick={handleStartReconciliation}
                disabled={isPending}
                className="w-1/2 h-12 rounded-xl font-bold shadow-sm"
              >
                {isPending ? "Đang xử lý..." : "Xác nhận & Bắt đầu đối soát"}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 3: Commit**

```bash
git add web/src/features/shift/components/cash-movement-dialog.tsx web/src/features/shift/components/open-shift-dashboard.tsx
git commit -m "feat(web): add open shift dashboard with blind count boundary and cash movement dialog"
```

---

### Task 8: Closing Reconciliation Dashboard & Recount/QR Dialogs

**Files:**
- Create: `web/src/features/shift/components/closing-reconciliation-view.tsx`
- Create: `web/src/features/shift/components/cash-count-dialog.tsx`
- Create: `web/src/features/shift/components/qr-observation-dialog.tsx`

**Interfaces:**
- Consumes: `useRecordCashCount`, `useRecordQRObservation`, `newRequestId`
- Produces: `<ClosingReconciliationView shift={shift} />`, `<CashCountDialog />`, `<QRObservationDialog />`

- [ ] **Step 1: Implement `cash-count-dialog.tsx`**

Create `web/src/features/shift/components/cash-count-dialog.tsx`:
```tsx
import { useState } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DenominationCalculator } from "./denomination-calculator";
import {
  type DenominationCounts,
  calculateDenominationTotal,
} from "@/features/shift/utils/denomination";
import { useRecordCashCount } from "@/features/shift/api/use-shift";
import { newRequestId } from "@/lib/command";
import { formatVND } from "@/lib/utils";
import { playClick, playSuccess, playError } from "@/lib/sound";

interface CashCountDialogProps {
  shiftId: string;
  isOpen: boolean;
  onClose: () => void;
}

export function CashCountDialog({ shiftId, isOpen, onClose }: CashCountDialogProps) {
  const { recordCashCount, isPending } = useRecordCashCount(shiftId);
  const [counts, setCounts] = useState<DenominationCounts>({});
  const [error, setError] = useState<string | null>(null);

  if (!isOpen) return null;

  const countedCash = calculateDenominationTotal(counts);

  const handleSubmit = async () => {
    playClick();
    if (countedCash < 0) {
      playError();
      setError("Số tiền không hợp lệ");
      return;
    }

    try {
      await recordCashCount({
        request_id: newRequestId(),
        counted_cash_vnd: countedCash,
      });
      playSuccess();
      onClose();
    } catch (err: unknown) {
      playError();
      setError(err instanceof Error ? err.message : "Không thể ghi nhận kiểm đếm lại");
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-lg overflow-hidden flex flex-col p-6 gap-4">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-base font-bold text-foreground">Đếm lại tiền mặt (Recount)</h3>
            <p className="text-xs text-muted-foreground">Ghi nhận số lần đếm mới vào biên bản đối soát</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg text-muted-foreground hover:bg-muted transition"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <DenominationCalculator counts={counts} onChange={setCounts} />

        <div className="p-3.5 rounded-xl bg-primary/10 border border-primary/20 flex items-center justify-between">
          <span className="text-xs font-semibold text-primary">Tổng tiền kiểm đếm lần này:</span>
          <span className="font-mono text-lg font-bold text-primary">{formatVND(countedCash)}</span>
        </div>

        {error && <p className="text-xs text-destructive text-center font-medium">{error}</p>}

        <div className="flex items-center gap-3 mt-2">
          <Button type="button" variant="outline" onClick={onClose} className="w-1/2 h-12 rounded-xl">
            Huỷ
          </Button>
          <Button
            type="button"
            onClick={handleSubmit}
            disabled={isPending}
            className="w-1/2 h-12 rounded-xl font-bold shadow-sm"
          >
            {isPending ? "Đang ghi..." : "Ghi nhận số đếm"}
          </Button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Implement `qr-observation-dialog.tsx`**

Create `web/src/features/shift/components/qr-observation-dialog.tsx`:
```tsx
import { useState } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useRecordQRObservation } from "@/features/shift/api/use-shift";
import { newRequestId } from "@/lib/command";
import { formatVND } from "@/lib/utils";
import { playClick, playSuccess, playError } from "@/lib/sound";

interface QRObservationDialogProps {
  shiftId: string;
  isOpen: boolean;
  onClose: () => void;
  initialReceived?: number;
  initialRefunded?: number;
}

export function QRObservationDialog({
  shiftId,
  isOpen,
  onClose,
  initialReceived = 0,
  initialRefunded = 0,
}: QRObservationDialogProps) {
  const { recordQRObservation, isPending } = useRecordQRObservation(shiftId);
  const [received, setReceived] = useState(initialReceived.toString());
  const [refunded, setRefunded] = useState(initialRefunded.toString());
  const [error, setError] = useState<string | null>(null);

  if (!isOpen) return null;

  const numReceived = parseInt(received.replace(/\D/g, ""), 10) || 0;
  const numRefunded = parseInt(refunded.replace(/\D/g, ""), 10) || 0;

  const handleSubmit = async () => {
    playClick();
    if (numReceived < 0 || numRefunded < 0) {
      playError();
      setError("Số tiền quan sát không được âm");
      return;
    }

    try {
      await recordQRObservation({
        request_id: newRequestId(),
        observed_received_vnd: numReceived,
        observed_refunded_vnd: numRefunded,
      });
      playSuccess();
      onClose();
    } catch (err: unknown) {
      playError();
      setError(err instanceof Error ? err.message : "Lỗi ghi nhận quan sát VietQR");
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-md overflow-hidden flex flex-col p-6 gap-4">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-base font-bold text-foreground">Đối soát VietQR qua App Ngân Hàng</h3>
            <p className="text-xs text-muted-foreground">Nhập số tiền thực tế ghi nhận từ biến động số dư tài khoản</p>
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
          <label className="text-xs font-semibold text-muted-foreground">
            Tổng tiền VietQR đã nhận (VND)
          </label>
          <Input
            type="text"
            inputMode="numeric"
            value={received}
            onChange={(e) => setReceived(e.target.value)}
            className="font-mono text-lg font-bold h-12"
          />
        </div>

        <div className="space-y-1.5">
          <label className="text-xs font-semibold text-muted-foreground">
            Tổng tiền VietQR đã hoàn trả (VND)
          </label>
          <Input
            type="text"
            inputMode="numeric"
            value={refunded}
            onChange={(e) => setRefunded(e.target.value)}
            className="font-mono text-lg font-bold h-12"
          />
        </div>

        {error && <p className="text-xs text-destructive text-center font-medium">{error}</p>}

        <div className="flex items-center gap-3 mt-2">
          <Button type="button" variant="outline" onClick={onClose} className="w-1/2 h-12 rounded-xl">
            Huỷ
          </Button>
          <Button
            type="button"
            onClick={handleSubmit}
            disabled={isPending}
            className="w-1/2 h-12 rounded-xl font-bold shadow-sm"
          >
            {isPending ? "Đang ghi..." : "Xác nhận đối soát QR"}
          </Button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Implement `closing-reconciliation-view.tsx`**

Create `web/src/features/shift/components/closing-reconciliation-view.tsx`:
```tsx
import { useState } from "react";
import {
  Wallet,
  ArrowDownRight,
  ArrowUpRight,
  QrCode,
  RotateCcw,
  CheckCircle2,
  AlertTriangle,
  Lock,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { CashCountDialog } from "./cash-count-dialog";
import { QRObservationDialog } from "./qr-observation-dialog";
import { CloseShiftDialog } from "./close-shift-dialog";
import { DIMENSION_LABELS } from "@/features/shift/utils/discrepancy";
import { formatVND } from "@/lib/utils";
import type { ShiftCurrentShiftResponse } from "@/api/generated/models";

interface ClosingReconciliationViewProps {
  shift: ShiftCurrentShiftResponse;
}

export function ClosingReconciliationView({ shift }: ClosingReconciliationViewProps) {
  const recon = shift.reconciliation;
  const [recountOpen, setRecountOpen] = useState(false);
  const [qrOpen, setQrOpen] = useState(false);
  const [closeDialogOpen, setCloseDialogOpen] = useState(false);

  if (!recon) return null;

  const canClose = recon.preview.can_close;

  return (
    <div className="space-y-6 max-w-5xl mx-auto p-4 sm:p-6 animate-in fade-in duration-200">
      {/* Header Bar */}
      <div className="p-6 rounded-2xl border border-border bg-card shadow-xs flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 mb-1.5">
            <Badge variant="outline" className="border-amber-500 text-amber-600 bg-amber-50 font-bold">
              Đang đối soát kết ca (CLOSING)
            </Badge>
            <span className="text-xs font-mono text-muted-foreground">ID: {shift.id.slice(0, 8)}</span>
          </div>
          <p className="text-xs text-muted-foreground">
            Bắt đầu đối soát lúc: {new Date(recon.started_at).toLocaleTimeString("vi-VN")} bởi {recon.starter.display_name}
          </p>
        </div>

        <div className="flex items-center gap-3">
          <Button
            type="button"
            onClick={() => setCloseDialogOpen(true)}
            className={`min-h-12 h-12 px-6 rounded-xl font-bold shadow-sm ${
              canClose
                ? "bg-emerald-600 hover:bg-emerald-700 text-white"
                : "bg-amber-600 hover:bg-amber-700 text-white"
            }`}
          >
            <Lock className="w-4 h-4 mr-2" />
            {canClose ? "Kết ca chính xác (Khớp 100%)" : "Kết ca có chênh lệch"}
          </Button>
        </div>
      </div>

      {/* Key Frozen Metrics */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <div className="p-4 rounded-xl border border-border bg-card shadow-2xs space-y-1">
          <span className="text-xs text-muted-foreground flex items-center gap-1.5">
            <Wallet className="w-3.5 h-3.5 text-primary" /> Tiền đầu ca
          </span>
          <p className="font-mono text-lg font-bold text-foreground">{formatVND(recon.opening_float_vnd)}</p>
        </div>
        <div className="p-4 rounded-xl border border-border bg-card shadow-2xs space-y-1">
          <span className="text-xs text-muted-foreground flex items-center gap-1.5">
            <ArrowDownRight className="w-3.5 h-3.5 text-emerald-600" /> Nộp thêm (Pay In)
          </span>
          <p className="font-mono text-lg font-bold text-emerald-600">+{formatVND(recon.pay_in_vnd)}</p>
        </div>
        <div className="p-4 rounded-xl border border-border bg-card shadow-2xs space-y-1">
          <span className="text-xs text-muted-foreground flex items-center gap-1.5">
            <ArrowUpRight className="w-3.5 h-3.5 text-rose-600" /> Rút chi (Pay Out)
          </span>
          <p className="font-mono text-lg font-bold text-rose-600">-{formatVND(recon.pay_out_vnd)}</p>
        </div>
        <div className="p-4 rounded-xl border border-border bg-card shadow-2xs space-y-1">
          <span className="text-xs text-muted-foreground flex items-center gap-1.5">
            <QrCode className="w-3.5 h-3.5 text-primary" /> Thu VietQR hệ thống
          </span>
          <p className="font-mono text-lg font-bold text-foreground">{formatVND(recon.expected_manual_qr_received_vnd)}</p>
        </div>
      </div>

      {/* Comparison Dimensions Table */}
      <div className="border border-border rounded-2xl bg-card overflow-hidden shadow-xs">
        <div className="p-4 border-b border-border bg-muted/30 flex items-center justify-between">
          <h3 className="text-sm font-bold text-foreground">Bảng đối soát 3 chiều doanh thu & quỹ</h3>
          <div className="flex gap-2">
            <Button type="button" variant="outline" size="sm" onClick={() => setRecountOpen(true)} className="h-8 text-xs">
              <RotateCcw className="w-3.5 h-3.5 mr-1" /> Đếm lại tiền mặt
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => setQrOpen(true)} className="h-8 text-xs">
              <QrCode className="w-3.5 h-3.5 mr-1" /> Cập nhật QR
            </Button>
          </div>
        </div>

        <div className="divide-y divide-border/60">
          {recon.preview.dimensions.map((dim) => {
            const diff = dim.difference_vnd ?? 0;
            const isExact = diff === 0;
            return (
              <div key={dim.dimension} className="p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-sm">
                <div className="space-y-0.5">
                  <span className="font-semibold text-foreground">{DIMENSION_LABELS[dim.dimension]}</span>
                  <div className="text-xs text-muted-foreground">
                    Kỳ vọng: <span className="font-mono">{formatVND(dim.expected_vnd)}</span> • Thực tế: <span className="font-mono font-semibold text-foreground">{dim.observed_vnd !== null && dim.observed_vnd !== undefined ? formatVND(dim.observed_vnd) : "Chưa có"}</span>
                  </div>
                </div>

                <div className="flex items-center gap-3">
                  <div className="text-right">
                    <span className="text-xs text-muted-foreground block">Chênh lệch:</span>
                    <span className={`font-mono font-bold text-base ${isExact ? "text-emerald-600" : "text-rose-600"}`}>
                      {diff > 0 ? `+${formatVND(diff)}` : formatVND(diff)}
                    </span>
                  </div>

                  {isExact ? (
                    <Badge variant="default" className="bg-emerald-100 text-emerald-800 border-emerald-200">
                      <CheckCircle2 className="w-3.5 h-3.5 mr-1" /> Khớp
                    </Badge>
                  ) : (
                    <Badge variant="destructive" className="bg-rose-100 text-rose-800 border-rose-200">
                      <AlertTriangle className="w-3.5 h-3.5 mr-1" /> Lệch
                    </Badge>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Sub-modals */}
      <CashCountDialog shiftId={shift.id} isOpen={recountOpen} onClose={() => setRecountOpen(false)} />
      <QRObservationDialog shiftId={shift.id} isOpen={qrOpen} onClose={() => setQrOpen(false)} />
      <CloseShiftDialog shift={shift} isOpen={closeDialogOpen} onClose={() => setCloseDialogOpen(false)} />
    </div>
  );
}
```

- [ ] **Step 4: Commit**

```bash
git add web/src/features/shift/components/cash-count-dialog.tsx web/src/features/shift/components/qr-observation-dialog.tsx web/src/features/shift/components/closing-reconciliation-view.tsx
git commit -m "feat(web): add closing reconciliation dashboard and recount/qr observation dialogs"
```

---

### Task 9: Shift Closure Dialog & Final Resolution

**Files:**
- Create: `web/src/features/shift/components/close-shift-dialog.tsx`

**Interfaces:**
- Consumes: `useCloseShift`, `useManagerApprovalStore`, `deriveNonZeroDimensions`, `newRequestId`
- Produces: `<CloseShiftDialog shift={shift} isOpen={isOpen} onClose={onClose} />`

- [ ] **Step 1: Implement `close-shift-dialog.tsx`**

Create `web/src/features/shift/components/close-shift-dialog.tsx`:
```tsx
import { useState } from "react";
import { X, Lock, AlertTriangle, CheckCircle2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useCloseShift } from "@/features/shift/api/use-shift";
import { useManagerApprovalStore } from "@/stores/use-manager-approval-store";
import {
  DIMENSION_LABELS,
  REASON_LABELS,
  deriveNonZeroDimensions,
} from "@/features/shift/utils/discrepancy";
import { newRequestId } from "@/lib/command";
import { formatVND } from "@/lib/utils";
import { playClick, playSuccess, playError } from "@/lib/sound";
import type {
  ShiftCurrentShiftResponse,
  ShiftCloseDiscrepancyInput,
  ShiftCloseDiscrepancyInputReason,
} from "@/api/generated/models";

interface CloseShiftDialogProps {
  shift: ShiftCurrentShiftResponse;
  isOpen: boolean;
  onClose: () => void;
}

export function CloseShiftDialog({ shift, isOpen, onClose }: CloseShiftDialogProps) {
  const { closeShift, isPending } = useCloseShift(shift.id);
  const promptApproval = useManagerApprovalStore((s) => s.promptApproval);

  const recon = shift.reconciliation;
  const nonZeroDims = recon ? deriveNonZeroDimensions(recon.preview.dimensions) : [];
  const hasDiscrepancy = nonZeroDims.length > 0;

  const [reasons, setReasons] = useState<Record<string, ShiftCloseDiscrepancyInputReason>>(() => {
    const initial: Record<string, ShiftCloseDiscrepancyInputReason> = {};
    for (const d of nonZeroDims) {
      initial[d.dimension] =
        d.dimension === "CASH" ? "CASH_COUNT_DIFFERENCE" : "QR_OBSERVATION_DIFFERENCE";
    }
    return initial;
  });

  const [notes, setNotes] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);

  if (!isOpen || !recon) return null;

  // Final IDs from latest attempts
  const latestCashCount = recon.cash_counts[recon.cash_counts.length - 1];
  const latestQR = recon.qr_observations[recon.qr_observations.length - 1];

  const handleClose = async () => {
    playClick();

    if (!latestCashCount) {
      playError();
      setError("Cần ít nhất một lần kiểm đếm tiền mặt");
      return;
    }
    if (!latestQR) {
      playError();
      setError("Cần ít nhất một lần quan sát đối soát VietQR");
      return;
    }

    try {
      let approverLoginCode: string | undefined;
      let managerPin: string | undefined;

      const discrepancies: ShiftCloseDiscrepancyInput[] = nonZeroDims.map((d) => ({
        dimension: d.dimension,
        reason: reasons[d.dimension] || "UNEXPLAINED",
        note: notes[d.dimension]?.trim() ? notes[d.dimension].trim() : undefined,
      }));

      if (hasDiscrepancy) {
        // Collect Manager approval
        const creds = await promptApproval({
          title: "Duyệt Kết Ca Có Chênh Lệch",
          description: "Ca làm việc có chênh lệch tiền mặt hoặc VietQR. Cần Quản lý xác nhận đóng ca.",
          confirmLabel: "Xác nhận đóng ca lệch",
        });
        approverLoginCode = creds.approverLoginCode;
        managerPin = creds.managerPin;
      }

      await closeShift({
        request_id: newRequestId(),
        final_cash_count_id: latestCashCount.id,
        final_qr_observation_id: latestQR.id,
        discrepancies,
        approver_login_code: approverLoginCode,
        manager_pin: managerPin,
      });

      playSuccess();
      onClose();
    } catch (err: unknown) {
      playError();
      if (err instanceof Error && err.message === "MANAGER_APPROVAL_CANCELLED") {
        return;
      }
      setError(err instanceof Error ? err.message : "Lỗi kết ca làm việc");
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-xs animate-in fade-in duration-200">
      <div className="bg-card border border-border shadow-xl rounded-2xl w-full max-w-lg overflow-hidden flex flex-col p-6 gap-4">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-base font-bold text-foreground">Xác nhận Kết thúc Ca làm việc</h3>
            <p className="text-xs text-muted-foreground">Đóng ca và chốt sổ bàn giao quầy thu ngân</p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-lg text-muted-foreground hover:bg-muted transition"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {hasDiscrepancy ? (
          <div className="space-y-4">
            <div className="p-3.5 rounded-xl border border-amber-200 bg-amber-50 text-amber-900 flex items-start gap-3">
              <AlertTriangle className="w-5 h-5 text-amber-600 shrink-0 mt-0.5" />
              <div className="text-xs space-y-1">
                <span className="font-bold block">Phát hiện chênh lệch số dư đối soát:</span>
                Vui lòng chọn lý do giải trình cho từng khoản chênh lệch. Quản lý cần nhập mã PIN phê duyệt để đóng ca.
              </div>
            </div>

            {/* List of discrepancies */}
            <div className="space-y-3">
              {nonZeroDims.map((dim) => {
                const diff = dim.difference_vnd ?? 0;
                return (
                  <div key={dim.dimension} className="p-3 rounded-xl border border-border bg-muted/20 space-y-2">
                    <div className="flex items-center justify-between text-xs font-semibold">
                      <span>{DIMENSION_LABELS[dim.dimension]}</span>
                      <span className="font-mono text-rose-600">{formatVND(diff)}</span>
                    </div>

                    <select
                      value={reasons[dim.dimension] || "UNEXPLAINED"}
                      onChange={(e) =>
                        setReasons({
                          ...reasons,
                          [dim.dimension]: e.target.value as ShiftCloseDiscrepancyInputReason,
                        })
                      }
                      className="w-full h-9 px-2.5 rounded-lg border border-input bg-background text-xs font-medium"
                    >
                      {Object.entries(REASON_LABELS).map(([k, v]) => (
                        <option key={k} value={k}>
                          {v}
                        </option>
                      ))}
                    </select>

                    <Input
                      value={notes[dim.dimension] || ""}
                      onChange={(e) => setNotes({ ...notes, [dim.dimension]: e.target.value })}
                      placeholder="Ghi chú thêm về chênh lệch..."
                      className="h-8 text-xs"
                    />
                  </div>
                );
              })}
            </div>
          </div>
        ) : (
          <div className="p-4 rounded-xl border border-emerald-200 bg-emerald-50 text-emerald-900 flex items-center gap-3">
            <CheckCircle2 className="w-6 h-6 text-emerald-600 shrink-0" />
            <div className="text-xs space-y-1">
              <span className="font-bold text-sm block">Số liệu khớp 100%!</span>
              Tiền mặt và VietQR hoàn toàn trùng khớp với hệ thống. Có thể kết ca ngay mà không cần Quản lý duyệt chênh lệch.
            </div>
          </div>
        )}

        {error && <p className="text-xs text-destructive text-center font-medium">{error}</p>}

        <div className="flex items-center gap-3 mt-2">
          <Button type="button" variant="outline" onClick={onClose} className="w-1/2 h-12 rounded-xl">
            Huỷ
          </Button>
          <Button
            type="button"
            onClick={handleClose}
            disabled={isPending}
            className="w-1/2 h-12 rounded-xl font-bold shadow-sm"
          >
            <Lock className="w-4 h-4 mr-1.5" />
            {isPending ? "Đang xử lý..." : hasDiscrepancy ? "Yêu cầu Quản lý duyệt đóng ca" : "Kết ca ngay"}
          </Button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/features/shift/components/close-shift-dialog.tsx
git commit -m "feat(web): add CloseShiftDialog with exact and discrepant closure flows"
```

---

### Task 10: Wire `ShiftView` Container into Route

**Files:**
- Modify: `web/src/features/shift/components/shift-view.tsx`
- Verify: `web/src/routes/_app/shift.tsx`

**Interfaces:**
- Consumes: `useCurrentShift`, `NoActiveShiftView`, `OpenShiftDialog`, `OpenShiftDashboard`, `ClosingReconciliationView`
- Produces: Updated `<ShiftView />` mounted in `/_app/shift`

- [ ] **Step 1: Implement `shift-view.tsx`**

Replace `web/src/features/shift/components/shift-view.tsx`:
```tsx
import { useState } from "react";
import { AlertCircle, RefreshCw, Clock } from "lucide-react";
import { useCurrentShift } from "@/features/shift/api/use-shift";
import { NoActiveShiftView } from "./no-active-shift-view";
import { OpenShiftDialog } from "./open-shift-dialog";
import { OpenShiftDashboard } from "./open-shift-dashboard";
import { ClosingReconciliationView } from "./closing-reconciliation-view";
import { Button } from "@/components/ui/button";

export function ShiftView() {
  const { data: shift, isLoading, isError, error, refetch } = useCurrentShift();
  const [openShiftModalOpen, setOpenShiftModalOpen] = useState(false);

  if (isLoading) {
    return (
      <div className="flex flex-col items-center justify-center p-12 min-h-[60vh] gap-4">
        <div className="w-12 h-12 rounded-2xl bg-primary/10 text-primary flex items-center justify-center animate-pulse">
          <Clock className="w-6 h-6 animate-spin" />
        </div>
        <p className="text-sm text-muted-foreground font-medium">Đang tải thông tin ca làm việc...</p>
      </div>
    );
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center p-8 max-w-md mx-auto min-h-[60vh] text-center gap-4">
        <div className="w-12 h-12 rounded-2xl bg-destructive/10 text-destructive flex items-center justify-center">
          <AlertCircle className="w-6 h-6" />
        </div>
        <div className="space-y-1">
          <h3 className="text-base font-bold text-foreground">Không thể tải thông tin ca</h3>
          <p className="text-xs text-muted-foreground">{error?.message || "Đã xảy ra lỗi kết nối"}</p>
        </div>
        <Button type="button" variant="outline" onClick={() => refetch()} className="rounded-xl h-10">
          <RefreshCw className="w-4 h-4 mr-2" />
          Thử lại
        </Button>
      </div>
    );
  }

  return (
    <div className="min-h-full pb-12">
      {/* State Router */}
      {!shift ? (
        <>
          <NoActiveShiftView onOpen={() => setOpenShiftModalOpen(true)} />
          <OpenShiftDialog
            isOpen={openShiftModalOpen}
            onClose={() => setOpenShiftModalOpen(false)}
          />
        </>
      ) : shift.state === "OPEN" ? (
        <OpenShiftDashboard shift={shift} />
      ) : shift.state === "CLOSING" ? (
        <ClosingReconciliationView shift={shift} />
      ) : (
        <NoActiveShiftView onOpen={() => setOpenShiftModalOpen(true)} />
      )}
    </div>
  );
}
```

- [ ] **Step 2: Typecheck and run all tests**

Run: `cd web && bun test && bun run build`
Expected: ALL tests pass, 0 type errors, build succeeds.

- [ ] **Step 3: Commit**

```bash
git add web/src/features/shift/components/shift-view.tsx
git commit -m "feat(web): wire full shift lifecycle view router"
```

---

### Task 11: UAT Gate & Operator Verification

**Files:**
- Plan file: `docs/superpowers/plans/2026-09-22-web-slice-2-shift-management.md`

**Step-by-step UAT Script:**

1. **Seed Development Environment**:
   Run: `make dev-seed`
   Ensure server runs at `localhost:8080` (`make run`) and web dev server at `localhost:5173` (`cd web && bun run dev`).

2. **Sign In**:
   - Go to `http://localhost:5173/auth/login`.
   - Sign in as Manager (`MGR01`, PIN: `123456`).
   - Declare workspace: `cashier`.

3. **Navigate to Shift (`/_app/shift`)**:
   - Verify empty state appears: "Chưa có ca làm việc nào đang mở".
   - Click "Mở ca làm việc ngay".

4. **Test Open Shift**:
   - In modal, enter 1,000,000 VND (try switching between direct input and denomination calculator).
   - Click "Xác nhận mở ca".
   - Verify shift transitions to `OPEN` state.
   - **Blind count check**: Verify no expected cash or sales revenue is visible on screen.

5. **Test Pay In (Nộp thêm tiền mặt)**:
   - Click "Nộp quỹ (Pay In)".
   - Enter 200,000 VND, reason "Bổ sung tiền thối lẻ".
   - Click "Yêu cầu Quản lý duyệt" -> Verify Manager Approval Dialog appears.
   - Enter Manager PIN `123456` -> Click "Xác nhận duyệt".
   - Verify success sound and notification.

6. **Test Pay Out (Rút quỹ tiền mặt)**:
   - Click "Rút quỹ (Pay Out)".
   - Enter 50,000 VND, reason "Lý do khác", enter note "Chi tiền đá viên".
   - Enter Manager PIN `123456`.
   - Verify success.

7. **Test Start Reconciliation (Bắt đầu kiểm tiền & Kết ca)**:
   - Click "Kiểm tiền & Kết ca".
   - Enter counted cash: 1,150,000 VND.
   - Click "Xác nhận & Bắt đầu đối soát".
   - Verify shift transitions to `CLOSING` state.
   - Verify reconciliation dashboard unlocks with expected vs observed metrics!

8. **Test Cash Recount & QR Observation**:
   - Click "Đếm lại tiền mặt" -> submit 1,150,000 VND.
   - Click "Cập nhật QR" -> input VietQR received = 0, refunded = 0.
   - Verify comparison table updates.

9. **Test Shift Closure**:
   - Click "Kết ca".
   - If exact: verify one-tap close.
   - If discrepancy: verify discrepancy reason dropdown appears, Manager Approval dialog collects PIN, and shift successfully closes.

- [ ] **Step 4: Record UAT Results & Mark Done**
