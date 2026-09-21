# Design Specification: Web Slice 2 — Sales Shift Management (`./web`, Phase 11 UI)

- **Author:** Antigravity & Team
- **Date:** 2026-09-22
- **Status:** Draft, pending review
- **Phase:** Phase 11, UI half — see [`docs/backlog/phase-11-clients-over-lan-and-single-binary.md`](../../backlog/phase-11-clients-over-lan-and-single-binary.md)
- **Predecessors:**
  - [`2026-09-21-web-frontend-slice-sequence-design.md`](2026-09-21-web-frontend-slice-sequence-design.md) (slice order & definition of done)
  - [`2026-09-21-web-slice-1-staff-access-session.md`](../plans/2026-09-21-web-slice-1-staff-access-session.md) (session store, auth seam, unwrap, design system)
- **Visual authority:** [`design-system/pos-cafe/pages/shift.html`](../../../design-system/pos-cafe/pages/shift.html)
- **Domain authority:** [`CONTEXT.md`](../../../CONTEXT.md), [`spec/decisions.md`](../../../spec/decisions.md), `internal/shift/` (Phase 07 backend implementation)

---

## 1. Purpose & Scope

Slice 2 delivers the **Sales Shift Management** screen and workflows in `web/src/features/shift/`, replacing the placeholder in `web/src/routes/_app/shift.tsx` with a real, API-backed interface against the Go `/shifts/*` endpoints.

No cashier station may process orders or receive money without an active Sales Shift. Slice 2 provides the accountability foundation for all subsequent POS operations (Slices 3, 4, 5).

### 1.1 In Scope

1. **Shared Infrastructure Primitives**:
   - `web/src/lib/command.ts`: `newRequestId()` helper generating intent-scoped UUID v4 for mutation idempotency.
   - `web/src/stores/use-manager-approval-store.ts`: Zustand store managing imperative Manager Approval requests.
   - `web/src/components/feedback/manager-approval-dialog.tsx`: Touch-friendly dialog collecting Manager approval (`approver_login_code` + `manager_pin`).
2. **Shift API Seam (`web/src/features/shift/api/use-shift.ts`)**:
   - Type-safe wrappers over generated Orval hooks (`GET /shifts/current`, `POST /shifts`, `POST /shifts/:id/cash-movements`, `POST /shifts/:id/reconciliation`, `POST /shifts/:id/reconciliation/cash-counts`, `POST /shifts/:id/reconciliation/qr-observations`, `POST /shifts/:id/close`).
3. **Interactive Shift Screen & Workflows**:
   - **No Active Shift**: Empty state prompting to open a shift.
   - **Open Shift**: Modal to set `opening_float_vnd` (with quick direct amount or denomination breakdown calculator).
   - **Active Shift (OPEN)**: Dashboard showing Shift ID, Opener, OpenedAt. Actions for Pay In (nộp tiền), Pay Out (rút chi tiền) requiring Manager Approval, and "Bắt đầu đối soát & Kết ca".
   - **Blind Count Boundary**: In `OPEN` state, expected cash, float amount, and revenue are strictly hidden per ADR-052.
   - **Reconciliation (CLOSING)**: Started with initial blind cash count. Displays frozen reconciliation snapshot, expected vs observed amounts across 3 dimensions (Cash, Manual QR Received, Manual QR Refunded), append recount, append QR observation.
   - **Close Shift**: Close exact if no discrepancies. If discrepancies exist, prompts for dimension reasons and collects Manager Approval before closing.
4. **Unit Tests**:
   - Pure logic tests with `bun test`: `command.test.ts`, `use-manager-approval-store.test.ts`, `denomination.test.ts`, `discrepancy.test.ts`.

### 1.2 Out of Scope (Non-Goals)

- **Thermal Receipt / Z-Report Printing**: Out of scope per `web-frontend-slice-sequence-design.md` section 7.
- **Closed Shift History Browsing (`GET /shifts`, `GET /shifts/:id`)**: Reserved for Manager audit inspection (ADR-052). Can be accessed in Slice 8/9.
- **Client-side Mirror Store**: Shift data is server-authoritative and cached via React Query; no `useShiftStore` is created (ADR-053).

---

## 2. Shared Infrastructure Primitives

### 2.1 `newRequestId` in `src/lib/command.ts`

To enforce the single-intent idempotency rule (spec sequence 4.3):
```typescript
import { v4 as uuidv4 } from "uuid"; // or crypto.randomUUID()

export function newRequestId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  // Fallback if needed
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === "x" ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}
```

A component initiating a mutating action creates a `requestId` once per operator intent (e.g. upon opening a dialog or initiating a submission) and retains it across retries and validation failures. Only upon successful execution or modal dismissal is a new `requestId` generated for the next action.

### 2.2 Manager Approval Store & Dialog

Actions such as Cash Movement (`Pay In`, `Pay Out`) and Discrepant Close require fresh Manager verification (`approver_login_code` and `manager_pin`).

#### Zustand Store: `src/stores/use-manager-approval-store.ts`
```typescript
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

interface ManagerApprovalState {
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

#### Global Dialog: `src/components/feedback/manager-approval-dialog.tsx`
- Mounted in `web/src/routes/_app.tsx`.
- Reads `isOpen`, `request`, `confirm`, `cancel` from `useManagerApprovalStore`.
- If current staff in `useSessionStore` has role `MANAGER`, pre-fills `approverLoginCode` with current staff's `login_code`, allowing one-touch PIN confirmation.
- If current staff is Cashier, provides an input for Manager's `login_code` and the touch `PinPad` component.
- On valid submission, calls `confirm({ approverLoginCode, managerPin })`.

---

## 3. Shift API Seam (`src/features/shift/api/use-shift.ts`)

Encapsulates all generated hooks from `src/api/generated/endpoints/shifts/shifts.ts` with envelope unwrapping via `unwrap`:

| Hook | Backend Endpoint | Method | Key Behavior |
| :--- | :--- | :--- | :--- |
| `useCurrentShift()` | `/shifts/current` | GET | Returns `CurrentShiftResponse \| null`. Stale time 15s. |
| `useOpenShift()` | `/shifts` | POST | Takes `{ request_id, opening_float_vnd }`. Invalidates current shift. |
| `useRecordCashMovement()` | `/shifts/:id/cash-movements` | POST | Takes `{ request_id, method, amount_vnd, reason, note, approver_login_code, manager_pin }`. |
| `useStartReconciliation()`| `/shifts/:id/reconciliation` | POST | Takes `{ request_id, counted_cash_vnd }`. Moves shift to CLOSING. |
| `useRecordCashCount()` | `/shifts/:id/reconciliation/cash-counts` | POST | Takes `{ request_id, counted_cash_vnd }`. Appends recount. |
| `useRecordQRObservation()`| `/shifts/:id/reconciliation/qr-observations` | POST | Takes `{ request_id, observed_received_vnd, observed_refunded_vnd }`. |
| `useCloseShift()` | `/shifts/:id/close` | POST | Takes `{ request_id, final_cash_count_id, final_qr_observation_id, discrepancies, approver_login_code?, manager_pin? }`. Moves shift to CLOSED. |

---

## 4. UI Architecture & Components

```
web/src/features/shift/
├── api/
│   └── use-shift.ts
├── utils/
│   ├── denomination.ts               # VND denomination arithmetic
│   ├── denomination.test.ts
│   ├── discrepancy.ts                # Discrepancy inspection & derivation
│   └── discrepancy.test.ts
└── components/
    ├── shift-view.tsx                 # Root container router by state
    ├── no-active-shift-view.tsx       # State 1: No active shift
    ├── open-shift-dialog.tsx          # Modal: Opening float entry
    ├── open-shift-dashboard.tsx       # State 2: Active shift (OPEN - blind count)
    ├── cash-movement-dialog.tsx       # Modal: Pay In / Pay Out
    ├── denomination-calculator.tsx    # Reusable bill counter widget
    ├── closing-reconciliation-view.tsx# State 3: Reconciliation (CLOSING)
    ├── cash-count-dialog.tsx          # Modal: Recount cash
    ├── qr-observation-dialog.tsx      # Modal: Update QR observations
    └── close-shift-dialog.tsx         # Modal: Final closure & discrepancy resolution
```

### 4.1 State-Driven Root: `ShiftView`

`ShiftView` calls `useCurrentShift()`.
- **Loading State**: Skeleton card layout.
- **Error State**: Error alert with retry button.
- **Data State**:
  - `data === null` or empty: renders `<NoActiveShiftView onOpen={() => setIsOpenShiftOpen(true)} />`.
  - `data.state === "OPEN"`: renders `<OpenShiftDashboard shift={data} />`.
  - `data.state === "CLOSING"`: renders `<ClosingReconciliationView shift={data} />`.

### 4.2 Blind Count Boundary (`OpenShiftDashboard`)

While `state === "OPEN"`, `GET /shifts/current` only provides `SalesShiftMetadata` (`id`, `state`, `opened_at`, `opener`).
- **Header Card**:
  - Badges: `Ca đang mở`, Opener name (`opener.display_name`), Opened time (`opened_at`).
  - Action buttons:
    - **Nộp tiền (Pay In)**: opens `CashMovementDialog` with `method: "PAY_IN"`.
    - **Rút tiền (Pay Out)**: opens `CashMovementDialog` with `method: "PAY_OUT"`.
    - **Kiểm tiền & Kết ca**: opens dialog to input initial counted cash float and start reconciliation.
- **Privacy Notice Card**:
  - Reminds staff that expected cash is kept hidden until reconciliation begins for fair and blind counting.

### 4.3 Cash Movement Flow (`CashMovementDialog`)

- Fields:
  - Loại giao dịch: `PAY_IN` (Nộp thêm quỹ) / `PAY_OUT` (Rút chi khẩn cấp).
  - Số tiền (VND): Numeric input formatted as currency with quick increment buttons (+50k, +100k, +200k, +500k).
  - Lý do (`reason`):
    - `ADD_CHANGE_FUND` ("Bổ sung tiền thối lẻ")
    - `REMOVE_EXCESS_FLOAT` ("Rút bớt tiền mặt dư thừa")
    - `SAFE_DROP` ("Chuyển tiền về két an toàn")
    - `OTHER` ("Lý do khác", requires non-empty `note`)
  - Ghi chú (`note`): optional textarea (mandatory if `OTHER`).
- Submission:
  1. Validates inputs.
  2. Calls `promptApproval({ title: "Xác nhận biến động quỹ", description: "Yêu cầu Quản lý duyệt thao tác nộp/rút tiền mặt." })`.
  3. On approval, executes mutation with `approverLoginCode` and `managerPin`.
  4. Plays sound feedback (`playSuccess()`) and closes modal.

### 4.4 Reconciliation Dashboard (`ClosingReconciliationView`)

Renders the frozen snapshot from `ReconciliationResponse`:
1. **Summary Cards (4 key metrics)**:
   - Tiền mặt ban đầu: `opening_float_vnd`
   - Biến động quỹ: `pay_in_vnd` - `pay_out_vnd`
   - Doanh thu tiền mặt hệ thống: `expected_cash_vnd`
   - Doanh thu VietQR hệ thống: `expected_manual_qr_received_vnd`
2. **Dimension Comparison Table (`preview.dimensions`)**:
   - **Tiền mặt (CASH)**: Expected vs Counted. Difference = Counted - Expected.
   - **VietQR Đã Nhận (MANUAL_QR_RECEIVED)**: Expected vs Observed.
   - **VietQR Hoàn Tiền (MANUAL_QR_REFUNDED)**: Expected vs Observed.
   - Action buttons per row: "Đếm lại tiền mặt" (Recount) / "Cập nhật quan sát QR" (Recheck QR).
3. **Attempt History Panels**:
   - Timeline list of `cash_counts[]` (sequence, counted amount, counter, timestamp).
   - Timeline list of `qr_observations[]` (sequence, received, refunded, observer, timestamp).
4. **Closure Action Bar**:
   - If `preview.can_close === true`: Green button "Kết ca chính xác (Khớp 100%)".
   - If `preview.can_close === false`: Amber button "Kết ca có chênh lệch (Cần duyệt)".

### 4.5 Discrepant Close Flow (`CloseShiftDialog`)

If any difference is non-zero (`preview.can_close === false`):
- Modal displays a summary of discrepancies for each dimension.
- Requires selecting a `reason` for each non-zero dimension:
  - `CASH_COUNT_DIFFERENCE` ("Chênh lệch tiền mặt kiểm đếm")
  - `QR_OBSERVATION_DIFFERENCE` ("Chênh lệch đối soát VietQR")
  - `UNEXPLAINED` ("Chưa rõ nguyên nhân")
- Optional note per discrepancy.
- Clicking "Xác nhận kết ca" triggers `promptApproval(...)` for Manager PIN.
- Executes `postShiftsShiftIdClose` with `discrepancies` array and approval credentials.

---

## 5. Denomination Arithmetic (`denomination.ts`)

Standard Vietnamese Dong banknotes:
```typescript
export const VND_DENOMINATIONS = [
  500_000, 200_000, 100_000, 50_000, 20_000, 10_000, 5_000, 2_000, 1_000,
] as const;

export type DenominationCounts = Record<number, number>;

export function calculateDenominationTotal(counts: DenominationCounts): number {
  return Object.entries(counts).reduce((sum, [denom, count]) => {
    return sum + Number(denom) * Math.max(0, count || 0);
  }, 0);
}
```

---

## 6. Definition of Done Compliance

- [x] Calls real Go API through generated client; no mock data survives.
- [x] Loading, empty, and error states render from real `ApiError` envelope.
- [x] Route `/shift` guarded by `sales_shift.operate` capability (already mounted in `_app/shift.tsx`).
- [x] Every mutation carries a `request_id` generated per user intent.
- [x] Matches `design-system/pos-cafe/pages/shift.html` styling, layout density, and Vietnamese terminology.
- [x] Basic tests only with `bun test`: pure logic, utilities, and store transitions.
- [x] Concludes with a concrete UAT script for operator verification.

---

## 7. Testing & UAT Script

### 7.1 Automated Unit Tests
- `command.test.ts`: test `newRequestId` UUID v4 generation and intent stability.
- `use-manager-approval-store.test.ts`: test prompt, confirm, cancel promise lifecycles.
- `denomination.test.ts`: verify calculation of total cash from count mapping.
- `discrepancy.test.ts`: verify discrepancy mapping and payload generation.

### 7.2 Manual UAT Verification Steps
1. **Initial Clean State**: Navigate to `/shift` when no shift is open. Verify "Không có ca làm việc nào đang mở" and "Mở ca làm việc" button.
2. **Open Shift**: Click "Mở ca", enter 1,000,000 VND (using denomination table or direct input). Verify shift transitions to `OPEN` state.
3. **Blind Count Check**: Verify that expected cash and float are not disclosed on the screen.
4. **Pay In / Pay Out**: Record a Pay In of 200,000 VND with reason "Bổ sung tiền thối lẻ". Confirm Manager Approval prompt appears, enter Manager PIN, confirm success.
5. **Start Reconciliation**: Click "Kiểm tiền & Kết ca", enter 1,200,000 VND as counted cash. Shift transitions to `CLOSING`.
6. **Closing Preview**: Verify expected cash vs counted cash is shown.
7. **Discrepancy Resolution & Close**:
   - If count matches: close exact without manager PIN.
   - If count differs: select discrepancy reason, enter Manager PIN, verify shift closes and status updates to closed.
