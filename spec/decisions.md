# Architecture Decision Records (ADR) - POS Cafe Backend

This document records architectural and technical design decisions made during the migration from TypeScript to Golang.

---

## ADR-001: Multi-Role Model for Staff (`staff_operational_roles`)

* **Decision Date:** 2026-09-09
* **Status:** Accepted
* **Context:** The legacy system allowed an employee to hold multiple roles simultaneously (e.g., `MANAGER` concurrently acting as `CASHIER`, or `CASHIER` concurrently acting as `BARISTA`).
* **Decision:** Retain the multi-role model using the relation table `staff_operational_roles(staff_identity_id, role)`.
* **Consequences:** 100% backward-compatible with the React frontend and existing POS business authorization logic.

---

## ADR-002: PIN Hashing Algorithm with `bcrypt`

* **Decision Date:** 2026-09-09
* **Status:** Accepted
* **Context:** The legacy system used `argon2id` from a Node.js library. An optimal solution was required for the Go backend.
* **Decision:** Use the standard `golang.org/x/crypto/bcrypt` package with default/standard cost to hash and verify staff PINs (4–8 digits).
* **Consequences:** Optimizes performance and memory footprint on low-spec POS hardware (Celeron processors, 2–4GB RAM).

---

## ADR-003: Hybrid Token Authentication Mechanism (Bearer Header + Cookie)

* **Decision Date:** 2026-09-09
* **Status:** Accepted
* **Context:** Must flexibly accommodate Web SPA clients, Desktop Terminals, and embedded hardware applications.
* **Decision:** Support a hybrid mechanism:
* Upon successful login/unlock, the API returns `token` in the JSON response body and simultaneously sets an HTTP-only cookie.
* The `RequireAuth` middleware prioritizes reading the token from the `Authorization: Bearer <token>` header, falling back to the `staff_session_token` cookie if absent.


* **Consequences:** Maximum flexibility across all client types; straightforward testing via Postman/curl and Swagger UI.

---

## ADR-004: Explicit Column Size Constraints (`VARCHAR(n)`)

* **Decision Date:** 2026-09-09
* **Status:** Accepted
* **Context:** Although PostgreSQL stores `TEXT` and `VARCHAR` with equivalent RAM/disk efficiency, leaving column lengths unbounded at the database layer exposes the system to data bloat if clients submit oversized payloads.
* **Decision:** Enforce explicit length constraints across tables:
* `display_name`: `VARCHAR(120)`
* `login_code`: `VARCHAR(24)`
* `pin_hash`: `VARCHAR(72)` (fits bcrypt's 60-character output comfortably)
* `token_hash`: `VARCHAR(64)` (SHA-256 hex string)
* `role`, `state`, `active_workspace`: `VARCHAR(20)`


* **Consequences:** Reinforces data integrity directly at the database tier.

---

## ADR-005: Consolidation of Idempotency Tables into a Single Table (`idempotency_keys`)

* **Decision Date:** 2026-09-09
* **Status:** Accepted
* **Context:** The legacy TypeScript codebase provisioned a dedicated table per use case (`staff_identity_creation_requests`, `staff_identity_enabled_state_requests`, `staff_identity_role_replacement_requests`, `staff_identity_pin_reset_requests`), leading to the schema bloat anti-pattern and wasted resources.
* **Decision:**
* Replace all four legacy tables with a **single unified table** following standard Stripe / IETF patterns:
```sql
CREATE TABLE idempotency_keys (
    key UUID NOT NULL,                       -- request_id from frontend
    actor_id UUID,                           -- acting staff identity ID
    action VARCHAR(50) NOT NULL,             -- 'staff.create', 'staff.set_enabled', etc.
    request_hash VARCHAR(64) NOT NULL,       -- SHA-256 payload hash for mutation detection
    response_code INT NOT NULL,              -- HTTP status code (200, 201...)
    response_body JSONB NOT NULL,            -- Cached JSON payload to replay on retries
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (actor_id, key)
);

```


* Reuse this table across all future slices such as `shift`, `sales`, and `payments` without adding extra tables.


* **Consequences:**
* Highly concise database schema that eliminates redundant boilerplate.
* Enables instant response replaying during network instability or terminal double-clicks.
* Simplifies cleanup automation via a scheduled cron job (purging records older than 24 hours).

---

## ADR-006: Provision sớm schema Sales cho Phase 3 (`service_sessions`, `table_assignments`)
- **Ngày quyết định:** 2026-09-12
- **Trạng thái:** Accepted
- **Bối cảnh:** Theo `CONTEXT.md`, một Bàn "may be associated with one or more active Service Sessions". Read `overview` canonical của Tables phải trả về các Service Session đang chiếm bàn, tức là phụ thuộc vào `table_assignments` và `service_sessions` — hai bảng thuộc quyền sở hữu nghiệp vụ của `internal/sales` (Phase 5). Nếu chờ Phase 5, Phase 3 sẽ ship một contract API thiếu field và phải breaking change về sau.
- **Quyết định:**
  - Migration `000006` của Phase 3 tạo luôn `service_sessions` và `table_assignments`, kèm `COMMENT ON TABLE` ghi rõ quyền sở hữu thuộc `internal/sales` (Phase 5).
  - `service_sessions` lược bỏ **duy nhất** cột `sales_shift_id` vì `sales_shifts` là bảng của Phase 4. Phase 5 bổ sung bằng `ALTER TABLE service_sessions ADD COLUMN sales_shift_id UUID NOT NULL REFERENCES sales_shifts(id)`.
  - `internal/tables` **không** import `internal/sales`. Nó đọc occupancy qua query sqlc của riêng nó (`ListCurrentTableOccupants`), đúng nguyên tắc "dùng queries hoặc interface, đừng import struct" của MIGRATE_PLAN §4.1.
  - Phase 3 chỉ **đọc** hai bảng này, không ghi qua API. Test integration seed trực tiếp bằng SQL.
- **Hệ quả:**
  - Contract công khai của Tables hoàn chỉnh và ổn định ngay từ Phase 3; frontend không phải chịu breaking change khi Phase 5 lên.
  - Đổi lại, Phase 5 phải thực hiện đúng một thao tác `ALTER TABLE` bổ sung thay vì `CREATE TABLE`.

---

## ADR-007: Reaffirming the Shared `idempotency_keys` Table

* **Decision Date:** 2026-09-12
* **Status:** Accepted
* **Context:** ADR-005 mandated that all slices share a single `idempotency_keys` table. However, Phase 2 (Catalog) created a dedicated `catalog_mutation_requests` table, contradicting this decision and reintroducing the exact schema bloat anti-pattern that ADR-005 aimed to eliminate.
* **Decision:**
* Starting from Phase 3 onwards, all slices must write idempotency records to the shared `idempotency_keys` table, using fully qualified action names for the `action` column (e.g., `tables.create_table`, `tables.rename_table`, `tables.set_table_availability`).
* `catalog_mutation_requests` is acknowledged as a **historical exception**, not a precedent. Catalog will not be retrofitted during Phase 3; any cleanup will be handled as a separate task.
* Each slice continues to own its respective executor. Sharing the **database table** does not imply sharing the **helper logic**: `internal/tables` must not import idempotency helpers from `internal/auth`.


* **Consequences:**
* Prevents schema bloat by avoiding a new table for every slice.
* Preserves vertical slice boundaries at the code layer while consolidating the storage layer.

---

## ADR-008: Partial Expected Cash in Phase 4

* **Decision Date:** 2026-09-13
* **Status:** Accepted
* **Context:** The canonical Expected Cash formula is Opening Float plus Cash Payments and Pay Ins, less Cash Refunds and Pay Outs. The Cash Payment and Cash Refund terms read the `payments` table, which references `checks`, which in turn references `service_sessions` and order drafts — the whole chain is owned by Phase 5. Provisioning it in Phase 4 would replicate the most intricate constraint set in the system a phase early, for a figure Phase 4 cannot yet produce anyway.
* **Decision:**
* Phase 4 computes `expected_cash_vnd` as Opening Float plus Pay Ins less Pay Outs, on read, never stored.
* The API field ships in its final name and shape from Phase 4, so the response contract does not change when the formula completes.
* Phase 5 adds the Cash Payment and Cash Refund terms to the single `SumCashMovements`-adjacent computation in `internal/shift/current.go`.
* The Swagger description and the design spec both state that the figure is incomplete until Phase 5.
* **Consequences:**
* The public Shift API contract is complete and stable from Phase 4 onward.
* Until Phase 5 lands, `expected_cash_vnd` reflects fund movements only and must not be presented to staff as a reconciliation figure.
* The guard on the total is symmetric, because sustained Pay Outs can legitimately drive the partial figure negative.
* **Superseded in part by ADR-020** for the Cash Refund term.

---

## ADR-009: `auth.VerifyManagerApproval` as a Shared Second-Party Approval Primitive

* **Decision Date:** 2026-09-13
* **Status:** Accepted
* **Context:** Recording a Cash Movement requires approval by a second identity holding the Manager role, who authenticates inline with a login code and PIN. This differs from the Catalog pattern, which re-verifies the actor's own PIN. Phase 5 requires the identical mechanism for Refund, Payment Void, and Comp.
* **Decision:**
* The verification lives in `internal/auth` as `VerifyManagerApproval`, and slices supply only the capability the approver must hold.
* It locks the approver row with `FOR UPDATE`, runs PIN verification against a dummy hash when no identity matches so response timing reveals nothing, and reports one of four denial reasons.
* Callers collapse every denial reason to one client-visible code. The specific reason reaches the server log and the denial audit event only.
* **Consequences:**
* Security-critical verification is implemented and tested once rather than copied into each slice that needs it.
* This is a deliberate exception to the general rule that slices do not share helpers. It does not extend to idempotency helpers, which ADR-007 keeps slice-local.

---

## ADR-010: Decomposition of Phase 5 into Four Sub-Phases

* **Decision Date:** 2026-09-13
* **Status:** Accepted
* **Context:** The canonical Sales module is roughly 16,000 lines exposing eighteen commands, five reads, and seventy-eight error codes, against three operations in Phase 4. A single Phase 5 specification would be unreviewable and a single implementation plan unexecutable.
* **Decision:**
* Phase 5 ships as 5A (Service Session and Order Draft), 5B (Commit, Checks, Charge Allocations), 5C (Payments, Check restructuring, settlement), and 5D (Submit, Orders, Preparation Units, closure), each with its own spec, plan, and suite, in that fixed order.
* **Consequences:**
* Each sub-phase is reviewable and independently testable; the cost is four spec-and-plan cycles instead of one, and a public contract that ships with fields no sub-phase before its owner can fill.

---

## ADR-011: Service Number Allocation Scoped to a Sales Shift

* **Decision Date:** 2026-09-13
* **Status:** Accepted
* **Context:** The canonical implementation derives the number from six hexadecimal characters of the Session UUID against a permanently global unique index with a five-attempt retry, so collision probability grows with all Sessions ever recorded and ends in an unrecoverable `SERVICE_NUMBER_UNAVAILABLE` in the system's most frequent operation. `CONTEXT.md` defines the Service Number as an operational label, which needs to be unambiguous among Sessions currently being served, not unique for all time.
* **Decision:**
* Allocate `S%05d` sequentially within the Sales Shift, serialized by transaction-scoped advisory lock, with a `(sales_shift_id, service_number)` unique index as a safety net and a dedicated `sequence` column as the authoritative ordinal; the six-character check constraint is unchanged and `SERVICE_NUMBER_UNAVAILABLE` is not migrated.
* **Consequences:**
* No exhaustion mode, no retry loop, and a label staff can read aloud; the number is no longer globally unique, so any future cross-Shift reference must carry the Shift id alongside it.

---

## ADR-012: Sales Resolves Catalog Through Its Own SQL

* **Decision Date:** 2026-09-13
* **Status:** Accepted
* **Context:** Draft item validation needs each Menu Item's effective Modifier Groups, which `internal/catalog` already computes in the exported pure function `EffectiveGroupIDs`; MIGRATE_PLAN §4.1 forbids importing another slice, and ADR-006 established reading another slice's tables through one's own sqlc query.
* **Decision:**
* `internal/sales` expresses the resolution once, in SQL, in `sql/queries/sales.sql`, rather than importing the function or reimplementing it in Go; an integration test importing both packages pins the SQL result to `catalog.EffectiveGroupIDs` over shared fixtures including the inherited-and-excluded and excluded-and-direct cases.
* **Consequences:**
* Slice boundaries hold at the code layer and the duplication is one function against one query with a mechanical consistency check, rather than two hand-maintained Go copies.

---

## ADR-013: Monetary bounds follow `int64`, not the canonical JavaScript ceiling

* **Decision Date:** 2026-09-14
* **Status:** Accepted
* **Context:** The canonical `MAX_CHECK_CHARGE_VND` is `Number.MAX_SAFE_INTEGER`, and `check-totals.ts` guards every accumulation against it, because JavaScript numbers lose integer precision beyond that point. Go stores VND in `int64` against `BIGINT` columns and has no such limit; the constant is a foreign artifact, as 5A already found when it replaced the derived `MAX_DRAFT_QUANTITY` with a plain 9,999.
* **Decision:**
* `unit_price_vnd`, `total_vnd`, and `charge_vnd` are `BIGINT` constrained only to be positive (non-negative for a Check's charge); Go keeps explicit positivity and overflow guards, raising `LINE_TOTAL_OUT_OF_RANGE` and `CHECK_CHARGE_OUT_OF_RANGE`, because Go's arithmetic wraps silently rather than failing.
* No business ceiling is imposed on a Check's total.
* **Consequences:**
* No arbitrary limit propagates into a domain that does not need one, and the guards that remain exist for a reason that is true in Go.

---

## ADR-014: The `checks` state domain ships complete in 5B; settlement columns do not

* **Decision Date:** 2026-09-14
* **Status:** Accepted
* **Context:** 5B writes only `OPEN` Checks, but its Check-target query filters on `state = 'OPEN'`, and 5C introduces `SETTLED` and `MERGED` along with the columns evidencing them.
* **Decision:**
* Migration `000009` declares `CHECK (state IN ('OPEN', 'SETTLED', 'MERGED'))` from the start, while `settled_at`, `settled_by_staff_identity_id`, `settled_during_sales_shift_id`, `settled_staff_access_session_id`, `merged_into_check_id`, and the composite constraint tying them to the state are added by 5C.
* This follows 5A's treatment of the `COMMITTED` value in the `order_drafts` domain.
* **Consequences:**
* The state filter is meaningful rather than vacuous from 5B, and 5C adds columns without rewriting a constraint; the cost is that two state values are unreachable until 5C, which 5B's suite asserts explicitly.

---

## ADR-015: Commit locks Catalog rows `FOR SHARE`

* **Decision Date:** 2026-09-14
* **Status:** Accepted
* **Context:** The canonical Commit locks `menu_items`, `menu_item_sizes`, and `modifier_options` `FOR UPDATE` to prevent retirement or an availability change between validation and write. Under `FOR UPDATE`, two cashiers committing orders that share one popular item serialize against each other on the system's busiest path, for no correctness gain — Commit only reads those rows.
* **Decision:**
* Commit takes `FOR SHARE` on Catalog rows, in sorted id order, after the Sales rows.
* `internal/catalog`'s mutations take `FOR UPDATE` and are therefore still blocked for the duration of a Commit.
* 5A's single-row `FOR UPDATE` in the add-draft-item path is left unchanged rather than churned.
* **Consequences:**
* Concurrent Commits sharing menu items proceed in parallel while retirement and availability changes remain excluded; the Sales-before-Catalog lock order of 5A §10 is preserved, so no deadlock cycle is introduced.

---

## ADR-016: One Check lock protocol for every 5C command

* **Decision Date:** 2026-09-14
* **Status:** Accepted
* **Context:** The canonical source locks `checks`, `service_sessions`, and `sales_shifts` all `FOR UPDATE` in the Payment path, but only `FOR UPDATE OF checks` in the restructuring path, with a source comment noting that locking parent rows there "can create a reverse dependency when Payment already owns one of the affected Check rows" — a deadlock hazard documented rather than removed.
* **Decision:**
* All four 5C commands lock `checks` `FOR UPDATE` in ascending id order, and `service_sessions` and `sales_shifts` `FOR SHARE`.
* `FOR SHARE` matches what the commands do, which is read parent state to evaluate a precondition.
* **Consequences:**
* No deadlock cycle exists among the 5C commands or against 5A and 5B; two cashiers paying different Checks of one Session proceed in parallel; Session and Shift closure, which take `FOR UPDATE`, remain excluded for the duration of each transaction.

---

## ADR-017: Settlement is a consequence of Payment, evaluated as a zero balance

* **Decision Date:** 2026-09-14
* **Status:** Accepted
* **Context:** The canonical `evaluateCheckSettlement` takes three inputs — `balanceVnd`, `hasPendingRefund`, `hasCustomerExcess` — of which the latter two are supplied as compile-time `false` constants from a `NO_RECORDED_CHECK_CORRECTIONS` object, because Refund does not exist yet.
* **Decision:**
* 5C has no settlement command and no settlement route; a Payment that brings the balance to zero settles the Check in the same transaction, writing all four evidence columns and a separate `CHECK_SETTLED` audit event.
* The condition is written as `balance == 0`.
* **Consequences:**
* A fully paid but unsettled Check cannot exist, guarded by the database constraint and the read invariant from both sides; Refund, when it arrives, brings its own readiness definition rather than inheriting a pre-built extension point nobody has designed against.

---

## ADR-018: Sales error codes are keyed by condition, not by operation

* **Decision Date:** 2026-09-14
* **Status:** Accepted
* **Context:** The canonical source declares twenty-seven codes across Payments, Split, and Merge, of which six pairs differ only by an operation prefix for an identical condition (`SALES_SHIFT_NOT_OPEN_FOR_SPLIT` against `MERGE_SALES_SHIFT_NOT_OPEN`, both describing what 5A already calls `OPEN_SALES_SHIFT_REQUIRED`), and three more describe field-shape violations that 5A's conventions treat as request validation.
* **Decision:**
* 5C declares thirteen new codes, one per condition, reuses 5A's and 5B's codes where the condition is the same, and demotes field-shape codes to request validation.
* `CHECK_CREATION_FAILED` is not migrated, per 5B §6.4.
* The design spec carries the complete canonical-to-5C mapping table.
* **Consequences:**
* A client handles one code per situation instead of one per situation per operation; the trace back to the canonical source stays mechanical through the mapping table; distinguishing `CHECK_NOT_FOUND` from `CHECK_NOT_OPEN` — which the canonical source collapses into one empty `WHERE` result — additionally makes the failure actionable for a cashier.

---

## ADR-019: A Payment stores its own Sales Shift and no attestation column

* **Decision Date:** 2026-09-14
* **Status:** Accepted
* **Context:** The canonical `payments` table stores `salesShiftId` even though it is reachable through `check → service_session → sales_shift`, and the Manual QR command takes a `receiptObservedInBankApp` boolean that must be `true` for the Payment to exist.
* **Decision:**
* `sales_shift_id` is stored on the Payment, because the Shift in which the money reached the cashier is an independent fact — a Session opened in one Shift can be paid in the next, and the derived path would answer the reconciliation question wrongly.
* No `receipt_observed_in_bank_app` column is created; the attestation is required in the request body and recorded in the audit event's details.
* **Consequences:**
* Expected Cash is a single-table scan over a partial index; a Payment's Shift attribution survives any later change to its Session; and the database stores no column whose value is `true` on every row.

---

## ADR-020: Expected Cash gains its Cash Payment term in 5C; the Cash Refund term is deferred to Refund

* **Decision Date:** 2026-09-14
* **Status:** Accepted
* **Context:** ADR-008 shipped `expected_cash_vnd` as Opening Float plus Pay Ins less Pay Outs, and recorded that "Phase 5 adds the Cash Payment and Cash Refund terms". 5C is the first sub-phase that creates a Cash Payment, and no sub-phase of Phase 5 creates a Refund.
* **Decision:**
* `internal/shift` adds one sqlc query summing applied amounts of `CASH` Payments for a Shift, and `ComputeExpectedCash` becomes Opening Float plus Cash Payments and Pay Ins, less Pay Outs.
* The sum is over applied amounts rather than tendered amounts, per `CONTEXT.md`.
* The Cash Refund term is deferred to whichever phase introduces Refund, and the Swagger description names Refund as the outstanding dependency rather than "Phase 5".
* `internal/shift` reads the `payments` table through its own query and does not import `internal/sales`, following ADR-012.
* **Consequences:**
* Expected Cash becomes a usable reconciliation figure for every cafe that does not issue cash refunds, which is the current operating reality; the remaining gap is named precisely instead of being attributed to a phase that will close without filling it.

---

## ADR-021: A Check command locks the Shift that is open, not the one its Session was opened in

* **Decision Date:** 2026-09-15
* **Status:** Accepted
* **Context:** ADR-019 stores a Payment's `sales_shift_id` because the Shift in which money reached the cashier is an independent fact, and a Session opened in one Shift can be paid in the next. The 5C implementation nonetheless read both that attribution and the "Shift must be open" precondition by joining `checks → service_sessions → sales_shifts`, recovering exactly the derived value ADR-019 exists to avoid. The two disagree as soon as a Session outlives its Shift.
* **Decision:**
* `LockCheckForPayment` and `LockChecksForRestructuring` no longer join `sales_shifts`. They lock the Check `FOR UPDATE` and its Session `FOR SHARE`, as ADR-016 requires.
* A separate query, `LockOpenSalesShiftForShare`, takes the Shift whose state is `OPEN` `FOR SHARE` and returns its id. `sales_shift_only_one_open_unique` makes "the open Shift" unambiguous, so the query needs no ordering.
* The Shift a Payment is attributed to, the Shift its settlement records, and the Shift precondition every Check command evaluates are all that row.
* The lock order is unchanged: `checks`, then `service_sessions`, then `sales_shifts`. The Shift lock is a second statement rather than part of the join, but no transaction takes these in a different order, so ADR-016's no-deadlock-cycle guarantee holds.
* **Consequences:**
* A Session that outlives its Shift stays payable, and its Payments are attributed to the Shift that was open when the money arrived — which is what reconciliation reads and what ADR-019 promised.
* The precondition is now "a Shift is open" rather than "the Session's Shift is open", matching §6.4's wording and removing a rejection that no cashier could act on.
* `internal/sales` no longer carries a `ShiftStateOpen` literal: whether a Shift is open is answered by reading `sales_shifts`, not by comparing a string.

---

## ADR-022: Split and Merge run the same charge invariant and preconditions as Payment

* **Decision Date:** 2026-09-15
* **Status:** Accepted
* **Context:** 5B established that a Check's stored `charge_vnd` is a denormalization of the sum over its allocations, and that a disagreement is a defect surfacing as a logged 500 rather than a business state. Payment implemented that check. Split and Merge, which also rewrite a charge, trusted the stored value instead. The three commands also evaluated the same three preconditions in two different orders, so identical bad state produced different error codes depending on which endpoint was called.
* **Decision:**
* One assertion, `assertChargeMatchesAllocations`, recomputes a Check's charge and fails on disagreement. Every command that rewrites a charge runs it before computing anything from the stored value.
* One function, `checkPreconditions`, evaluates Check state, then Session state, then Shift state, in the precedence §6.2 documents. Every command calls it, so a given state yields one code across the whole surface.
* The invariant reads one aggregate query rather than the full allocation projection, so Payment no longer loads every allocation and its modifiers to compute a single sum.
* Split and Merge plan their whole allocation redistribution in Go and apply it in a fixed number of batched statements, so the work done while both Checks are locked does not grow with the number of items moved.
* **Consequences:**
* A charge that has drifted fails the command that would have built on it, instead of being propagated into a second Check before the read path notices.
* Split and Merge each cost one extra aggregate read inside the transaction; that is the price of not trusting a denormalization, and it is bounded.
* A Check that is `SETTLED` with its Session and Shift also closed now reports `CHECK_NOT_OPEN` from every endpoint, rather than `CHECK_NOT_OPEN` from Split and `SERVICE_SESSION_ALREADY_CLOSED` from Pay Cash.
