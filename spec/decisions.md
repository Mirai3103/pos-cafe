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
* **Status:** Accepted; **superseded in part by ADR-030** for the Session-lock clause.
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
* **Status:** Accepted; **superseded in part by ADR-030** for the Session-lock clause.
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

---

## ADR-023: Phase 5D borrows the Preparation Unit advance command from Phase 6

* **Decision Date:** 2026-09-15
* **Status:** Accepted
* **Context:** ADR-010 assigned Preparation Units to 5D meaning their creation at Submit, leaving every state transition to Phase 6. But closure requires every unit to be terminal, and a Completed Sale's `preparation_history` is made of those transitions, so without an advance command the closure branch would be unreachable through the API and the history would ship permanently empty.
* **Decision:**
* 5D implements the linear advance chain and nothing else of Phase 6.
* **Consequences:**
* Phase 5 closes as a genuinely deployable whole with no seeded fixtures; the cost is one command implemented one phase early, in the package that will own it anyway.

---

## ADR-024: `internal/preparation` is created in 5D

* **Decision Date:** 2026-09-15
* **Status:** Accepted
* **Context:** the advance command could live in `internal/sales`, which already carries forty-plus files and is gaining four tables in this sub-phase.
* **Decision:**
* create the package now, with the read/write boundary of §4.1.
* **Consequences:**
* Phase 6 grows into an existing package instead of extracting code out of `internal/sales`; the cost is a package holding one command, and a boundary that review must enforce because sqlc's single generated package cannot.

---

## ADR-025: `order_items` carries no commercial snapshot

* **Decision Date:** 2026-09-15
* **Status:** Accepted
* **Context:** the canonical table duplicates eight immutable columns from `committed_items` across a one-to-one foreign key.
* **Decision:**
* store the foreign key alone.
* **Consequences:**
* one source of truth and no possibility of divergence; the cost is a join on the Completed Sale and Order reads, and an intentional asymmetry with `preparation_units`, which snapshots for a read-path reason `order_items` does not have.

---

## ADR-026: Closure idempotency uses the shared executor

* **Decision Date:** 2026-09-15
* **Status:** Accepted
* **Context:** the canonical source maintains `completed_sale_closing_requests` because its idempotency helper is typed to the Service Session projection.
* **Decision:**
* the Go executor is generic over the result type, so closure uses it with `T = CompletedSaleResponse`.
* **Consequences:**
* one fewer table and one fewer replay path; closure's idempotency and audit behave identically to every other command's.

---

## ADR-027: Preparation history is a table, not a projection over `audit_events`

* **Decision Date:** 2026-09-15
* **Status:** Accepted
* **Context:** the canonical source reconstructs it by joining audit rows on a JSONB field cast to text and silently dropping unparseable rows.
* **Decision:**
* write `preparation_unit_transitions` in the same transaction as the advance, and keep the audit event alongside it.
* **Consequences:**
* a Completed Sale's immutable content rests on real foreign keys, real indexes, and a `CHECK`-enforced transition graph; the cost is one table and a deliberate, documented double write of the same moment to two records with different purposes.

---

## ADR-028: `preparation_units.state` declares all six canonical values in 5D

* **Decision Date:** 2026-09-15
* **Status:** Accepted
* **Context:** 5A guessed a partial `service_sessions.state` domain and had to correct it; 5C responded with ADR-014's complete-domain precedent.
* **Decision:**
* declare `QUEUED`, `IN_PREPARATION`, `READY`, `FULFILLED`, `CANCELLED`, `WASTED`, and write only the first four.
* **Consequences:**
* Phase 6 adds commands without a schema migration; the closure policy is written once against the complete domain.

---

## ADR-029: The pending-Refund closure check is not migrated

* **Decision Date:** 2026-09-15
* **Status:** Accepted
* **Context:** the canonical readiness function reports Checks carrying a pending Refund, but Refund is outside Phase 5 and `pending_refund_vnd` is absent from the contract by 5C's decision.
* **Decision:**
* omit the branch rather than stub it against a column that does not exist.
* **Consequences:**
* the closure policy has one fewer condition than canonical; it is restored together with Refund, and §6.4 records that the omission is deliberate.

---

## ADR-030: A Check command locks its Service Session `FOR UPDATE`

* **Decision Date:** 2026-09-15
* **Status:** Accepted; supersedes the Session-lock clause of ADR-016
* **Context:** ADR-016 weakened the canonical `FOR UPDATE` on `service_sessions` to `FOR SHARE` on the premise that the 5C commands "read parent state to evaluate a precondition". That premise is incomplete: every 5C command also rebuilds the whole Service Session read model via `LoadServiceSession` inside the same READ COMMITTED transaction, and `loadChecks` does it in several statements. A sibling Check's commit landing between those statements is observed half-applied and trips the settlement invariant, rolling back a valid Payment. Latent since 5C, and reproducible 3/3 once the integration suite shares one warm pool per package (the per-test connection handshake had been staggering the racing goroutines past the window).
* **Decision:**
* `LockCheckForPayment` locks the Check `FOR UPDATE` and its Session `FOR UPDATE` in two separate statements, Check first: SQL does not guarantee that one statement's `FOR UPDATE OF c, s` acquires the two relations' tuple locks in OF-list order, and the no-deadlock argument below depends on that order being explicit.
* `LockChecksForRestructuring` locks its Checks only and the caller takes the Session lock afterwards, so the lock order stays Check(s) then Session for every command and ADR-016's no-deadlock guarantee survives. Two Payments on one Session no longer proceed in parallel; they serialize.
* **Consequences:**
* Sibling-Check parallelism within a Session is given up deliberately. Payments on different Sessions, and every other command, are unaffected.
* **Known remaining hole, recorded rather than fixed:** the other commands that call `LoadServiceSession` (Commit, draft add/edit/remove) do not take the Session lock at all, so a concurrent Payment can still tear *their* read-model rebuild. No test exercises that pairing, and closing it belongs in its own change.

---

## ADR-031: Submit keeps its Session-then-Checks lock order, recording the Submit-Payment AB-BA window

* **Decision Date:** 2026-09-16
* **Status:** Accepted
* **Context:** Submit locks its source in the order the 5D plan mandates — the Service Session and its committed draft first, the Checks second — while ADR-030 has every 5C command lock the Check first and the Session second. A Submit racing a Payment on the same Session is therefore an AB-BA: Submit holds the Session and waits on the Check while Payment holds the Check and waits on the Session. §10's "serialize rather than deadlock" claim holds for the Check locks, which both sides take in ascending (created_at, id) order, but not for the Session lock. Before the final-fix split the window was one query round-trip wide and Task 11's `TestSubmitAgainstConcurrentPayment` ran clean over 30+ `-race` runs; splitting the Session lock from the draft lock widened it to the span of Submit's Session → Draft → Checks lock sequence — two intervening round trips — and the abort then fired twice in that test's first `-race` runs, with every later run stable (20/20 under the contract-aligned test). The exposure is real but narrow — and when PostgreSQL does abort one side with 40P01, the abort rolls the transaction back including its idempotency claim, so a client retry runs clean.
* **Decision:**
* Keep Submit's Session+Draft → Checks order. §6.1's step order and the idempotency/audit machinery all treat the locked source as the fact the mutation committed against; the window is accepted and recorded rather than reordered, and no lock retry is added.
* `TestSubmitAgainstConcurrentPayment` asserts this contract — either interleaving, any failure must be the 40P01 abort, no corruption — rather than asserting the absence of deadlock.
* The caveat lives where the window does, in the comment on `LockSubmittableDraft` in `sql/queries/sales.sql`.
* **Consequences:**
* A concurrent Payment racing a Submit on one Session can surface a single 500 (40P01) and succeed on retry; the rollback leaves no partial state and no stored result, so the retry is a first attempt, not a replay.
* The exposure stays bounded by the two-round-trip width of Submit's lock span and the empirical record — observed twice in the split's first runs, then 20/20 stable `-race` runs. If the window ever observably hurts, reordering Submit to Checks-then-Session remains open for Phase 6 as its own measured change.

---

## ADR-032: Phase 6 is decomposed

* **Decision Date:** 2026-09-16
* **Status:** Accepted
* **Context:** Preparation spans ordinary queue work, exceptional corrections, and Cancellation's unresolved Refund/Comp dependencies.
* **Decision:**
* ship 6A queue and ordinary transitions, 6B exceptional preparation workflows, and 6C cancellation with its financial dependencies.
* **Consequences:**
* ordinary operational work remains independent from correction and refund policy; Phase 6 is not complete when 6A ships.

---

## ADR-033: Submit remains the queue creation boundary

* **Decision Date:** 2026-09-16
* **Status:** Accepted
* **Context:** Phase 5D already creates Preparation Units atomically with Submit.
* **Decision:**
* do not publish or consume `order.submitted` through Watermill for queue creation.
* **Consequences:**
* a committed Submit is immediately queue-visible with no duplicate delivery path; Sales remains the sole creator while Preparation owns transitions.

---

## ADR-034: Bulk advance uses per-unit savepoints

* **Decision Date:** 2026-09-16
* **Status:** Accepted
* **Context:** stale or missing selected units are normal operational outcomes, while audit, database, and idempotency failures are not.
* **Decision:**
* verify authority and idempotency once, lock unique units in UUID order, and process each unit in a fixed package-private PostgreSQL savepoint.
* **Consequences:**
* missing and stale units become typed outcomes; infrastructure failures abort every successful unit in the outer transaction.

---

## ADR-035: Launch queue freshness uses polling

* **Decision Date:** 2026-09-16
* **Status:** Accepted
* **Context:** the staff-LAN client needs fresh shared queue state but no Phase 6A behavior requires server push.
* **Decision:**
* expose a side-effect-free GET with PostgreSQL `observed_at`; let Phase 7 poll and invalidate after local mutations.
* **Consequences:**
* Phase 6A adds no SSE, WebSockets, notifier seam, long-lived connections, or background fan-out.

---

## ADR-036: Phase 6B uses typed correction facts and typed transition history

* **Decision Date:** 2026-09-17
* **Status:** Accepted
* **Context:** Waste, Remake, State Correction, and Alerts are the exceptional half of preparation, and every one of them changes a unit's current state. Storing the facts as audit JSON, or reconstructing a Completed Sale's history by re-deriving states from audit payloads, would repeat the exact pattern ADR-027 rejected for the advance chain — real business content resting on unparseable, unparsed log rows.
* **Decision:**
* Waste, Remake, State Correction, and Alerts each receive an explicit append-only table (`preparation_wastes`, `preparation_remakes`, `preparation_state_corrections`, `preparation_alerts`), with CHECK-enforced reason catalogs and transition pairs.
* Every current-state change — forward advance, WASTED, one-step reverse — also writes a typed `preparation_unit_transitions` row in the same transaction, so a Completed Sale's history is read from tables and never reconstructed from audit JSON.
* **Consequences:**
* Correction history carries real foreign keys and database-enforced integrity; the cost is four tables and the deliberate, documented double write of each moment to a fact and an audit with different purposes (ADR-027's precedent extended to 6B).
* Replay and conflict detection stay on the shared `idempotency_keys` executor (ADR-026); facts are never consulted to answer an idempotency question.

---

## ADR-037: A Remake is a linked new unit and the only launch priority

* **Decision Date:** 2026-09-17
* **Status:** Accepted
* **Context:** A wasted drink must be remade without touching the customer's bill, and the bar must see the replacement ahead of ordinary FIFO work. A general rush priority — any unit boostable on request — would reintroduce the queue-jumping the FIFO lane exists to prevent and would need its own authorization policy.
* **Decision:**
* A Remake creates one linked new unit: it copies the source's immutable preparation snapshot, allocates the Order Item's next unit number under the Order Item lock, and links to the exact wasted source unit through `remake_of_preparation_unit_id`.
* REMAKE is the only priority. It is derived from the remake link rather than being a requestable field, and only active REMAKE units take the queue's priority lane.
* A Remake changes no Order Item, allocation, Check charge, Payment, Refund, or Comp, and one Waste carries at most one Remake, enforced by a database constraint.
* **Consequences:**
* Queue order stays explainable — linked remakes first, then FIFO — with no general rush priority to govern; the cost is that a barista cannot prioritize anything a Waste did not create.
* Every replacement is traceable to its wasted source forever, and the copy is the snapshot at waste time, not a re-resolution of the catalog.

---

## ADR-038: State Correction is an atomic one-step reverse command with Manager self re-authentication

* **Decision Date:** 2026-09-17
* **Status:** Accepted
* **Context:** A barista can advance a unit one step too far, and nothing can undo it. `VerifyManagerApproval` (ADR-009) exists for second-party approval of money movements, but a state correction only undoes a recorded mistake on the actor's own station — there is no second party whose approval the act needs, only certainty about who is acting.
* **Decision:**
* State Correction is one all-or-nothing command accepting 1 through 50 unique units, each moved exactly one step backward — QUEUED ← IN_PREPARATION ← READY ← FULFILLED — with no skipping and no multi-step reversal.
* The command requires the actor to hold Manager and re-authenticate with their own current PIN inside the mutation transaction, before the fingerprint is hashed and before any replay. It is self re-authentication, deliberately not ADR-009 second-party approval.
* The PIN is request-only: it never enters a fingerprint, stored result, database fact, audit, response, or log.
* **Consequences:**
* A stolen unlocked session cannot rewrite preparation history without the Manager's PIN, and PIN rotation or role revocation takes effect on the very next call — including replays — because the gate runs before the claim is consulted.
* The loser of a concurrent advance or correction re-reads the winner's committed state and the whole batch refuses; a closed Session refuses the whole batch before any unit is touched. Partial reversals cannot exist.
* **Supersedes nothing; complements ADR-009**, which remains the primitive for approving other people's financial acts.

---

## ADR-039: Alert acknowledgment controls terminal-unit queue visibility only

* **Decision Date:** 2026-09-17
* **Status:** Accepted
* **Context:** A WASTED unit is terminal the instant its Waste fact commits, so state-based queue membership would hide it before anyone has seen it; keeping terminal units visible forever would eventually bury the live queue under acknowledged history. Neither the queue's unit list nor the unit's state domain has room for a third answer.
* **Decision:**
* An unacknowledged WASTE alert retains its terminal unit on the queue, in a lane behind the live work; the acknowledgment records who saw it — identity, access session, time — and is the only act that removes the unit from the queue.
* Acknowledgment changes no unit state, transition, closure verdict, Check, Payment, or allocation; it writes its evidence and nothing else.
* Phase 6B writes only the WASTE kind; the reserved CANCELLATION and CHANGE kinds stay unwritten until 6C, without a schema migration.
* **Consequences:**
* Exceptional work can neither be silently lost nor buried by acknowledged history; alerts are the visibility mechanism, so no extra state is added to the unit.
* Closure ignores alerts entirely — a wasted unit is terminal for closure whether or not anyone acknowledged it — because the alert exists for the bar display, not for the closure policy.

---

## ADR-040: Cancellation is one cross-slice PostgreSQL consistency boundary

* **Decision Date:** 2026-09-18
* **Status:** Accepted
* **Context:** Cancellation changes a Preparation Unit's lifecycle and the customer's charge in one act, so splitting it across an event or a new orchestration package would leave a terminal unit and a reduced Check able to commit apart.
* **Decision:**
* Preparation owns the command, state transition, fact, and alert, and receives a narrow exception to write the live Charge Adjustment and Check charge atomically. No event or new orchestration package is introduced.
* **Consequences:**
* Cancel/Change requires `sales.operate`, not `preparation.operate`: it is initiated by Cashier work and changes customer charge; Manager also has the capability, and a Barista alone cannot cancel commercial work.
* Cancel or active Comp racing a later Submit on the same Session can reach the same narrow AB-BA window as Payment versus Submit; PostgreSQL `40P01` is an accepted retryable outcome for this pairing only (ADR-031), and rollback removes every business write and idempotency claim.
* Every successful Cancellation writes one terminal state, typed transition, typed fact, alert, and audit evidence atomically with the live Charge Adjustment; unchanged Check charge must equal base allocations less live adjustments.

---

## ADR-041: Charge reduction is append-only and has live versus post-sale scope

* **Decision Date:** 2026-09-18
* **Status:** Accepted
* **Context:** Cancellation and Comp must reduce what the customer owes without editing any immutable source fact, and a correction requested after closure must not rewrite the sale the customer already received.
* **Decision:**
* Cancellation and active Comp update the denormalized live Check charge; post-sale Comp links to Completed Sale and never rewrites its snapshot.
* **Consequences:**
* Every reduction is an append-only Charge Adjustment carrying `LIVE_CHECK` or `POST_SALE` scope, its exact source allocation, and the Shift it was recorded in.
* Completed Sale loading uses a structural closure boundary, not occurrence timestamps: a correction that observes `CLOSED` either rejects (Cancellation and Payment Void) or writes an explicit `POST_SALE` adjustment and Refund carrying `completed_sale_id` (Comp and Refund), and later facts appear only in the additive `post_sale_corrections` history.

---

## ADR-042: Refund consumes two independently locked capacities

* **Decision Date:** 2026-09-18
* **Status:** Accepted
* **Context:** Money leaves through the same method it arrived, so a Refund must reconcile both the corrected charge it is returning and the original receipt it is returning it through; verifying only one of them would let either side be spent twice.
* **Decision:**
* Every Refund allocates against both source Charge Adjustments and original non-voided Payments; pending Manual QR intents reserve both before money moves.
* **Consequences:**
* A Refund allocates equal positive totals against explicit source Adjustments and non-voided source Payments of one method and one Check, and a mixed-method source requires separate Refunds.
* Concurrent or pending Refunds cannot spend Adjustment or Payment refundable capacity twice, including by pending Manual QR intents.

---

## ADR-043: Refund completion is an append-only fact

* **Decision Date:** 2026-09-18
* **Status:** Accepted
* **Context:** Cash moves the moment staff record it, while a Manual QR Refund is approved before the outbound transfer exists, so completion time is real business content rather than a mutable status.
* **Decision:**
* Cash intent and completion commit together; Manual QR completion is appended after outbound confirmation, with no mutable Refund state column.
* **Consequences:**
* A Cash Refund always has a completion on successful command return; a Manual QR Refund does not until confirmed, and the approved amount and allocations cannot change during confirmation.
* Manual QR Refund confirmation requires current `sales.operate` but no second approval: it attests that the approved money movement occurred rather than approving a new one.

---

## ADR-044: A Check remains settled while Refund is pending

* **Decision Date:** 2026-09-18
* **Status:** Accepted
* **Context:** A Check's state records covered customer debt; once Cancellation or Comp reduces a paid Check's charge, the overpayment is an obligation to return rather than an unpaid balance, and ADR-029 deliberately omitted that branch until Refund existed.
* **Decision:**
* Check state records covered customer debt; pending Refund is a separate obligation that blocks Service Session closure, restoring ADR-029's omitted branch without adding a Check state.
* **Consequences:**
* Service Session closure rejects pending Refund after unsettled Checks and before work/preparation failures, through the new `PENDING_REFUND_FOR_CLOSURE` condition.
* A settled Check remains settled while carrying pending Refund and remains ineligible for split or merge, and a cancellation that settles an already-settled Check writes no second settlement evidence.

---

## ADR-045: Payment Void is whole, append-only, and open-original-Shift only

* **Decision Date:** 2026-09-18
* **Status:** Accepted
* **Context:** A Payment recorded in error must leave the reconciliation trail intact, and un-doing money from a closed Shift or a closed Session needs the deferred Post-Shift Payment Correction contract rather than an under-specified command here.
* **Decision:**
* It may reopen a Check but never edits the Payment; a closed Shift requires the deferred Post-Shift Payment Correction.
* **Consequences:**
* The Void is always for the entire applied amount and the source Payment is never edited or deleted; it reopens a Check when the remaining valid coverage no longer covers its charge and clears settlement evidence atomically, and leaves a covered Check settled.
* A refunded Payment, an already-voided Payment, a closed original Shift, a merged Check, or a closed Service Session rejects; it does not automatically create a replacement Payment, and staff record one through the ordinary Cash or Manual QR command.
* Expected Cash removes the voided Cash Payment term, and received totals remove the voided Manual QR Payment term.

---

## ADR-046: Expected Cash uses valid Cash Payments less completed Cash Refunds

* **Decision Date:** 2026-09-18
* **Status:** Accepted
* **Context:** The Shift read reconciles the drawer, so it must count money that actually moved: voids reverse a receipt, pending Refunds have not moved yet, and resolved post-sale corrections change what the drawer owes without rewriting receipt history.
* **Decision:**
* Cash Payment Voids remove their source term, pending Refunds do not move money, and `internal/shift` derives all terms through its own SQL.
* **Consequences:**
* Expected Cash is Opening Float + non-voided Cash Payments − completed Cash Refunds + Pay Ins − Pay Outs; Manual QR net received is `manual_qr_payment_vnd - manual_qr_payment_void_vnd`, and completed Refunds remain separately visible rather than silently changing received history.
* `internal/shift` derives every term with its own sqlc queries and imports no Sales or Preparation package.
* Because Shift Close is not implemented, Phase 6C reports pending Refund intents and unresolved post-sale correction amounts in the current-Shift read but does not add a Shift closure route or claim to enforce its future closure policy; a later Shift Close design must consume these authoritative fields rather than inventing another calculation.


---

## ADR-047: Migration closed; pos-cafe is its own authority

* **Decision Date:** 2026-09-18
* **Status:** Accepted
* **Context:** The Go system has passed its TypeScript source. The `cafe-pos` tracker `.scratch/opening-day-pos-v0/` holds seventeen implementation tickets, of which nine are completed there; tickets 07 (Split Checks and mixed settlement) and 11 (Correct charges and Payments without mutation) remain `ready-for-agent` in TypeScript while Go delivered them as Phase 5C and Phase 6C. From ticket 12 onward no canonical implementation exists on either side. Meanwhile fifteen citations of `cafe-pos/CONTEXT.md` across the approved Phase 0-6C specifications made the binding domain authority live in a repository this project neither owns nor tracks.
* **Decision:**
* `CONTEXT.md` is copied verbatim into this repository and is the binding domain authority. The fifteen citations are rewritten to the local path.
* References to `cafe-pos/src` and `cafe-pos/.scratch` inside Phase 0-6C specifications are historical provenance and carry no live authority. They are left exactly as written; rewriting them would falsify an approved record.
* Work from Phase 07 onward is designed from `CONTEXT.md`, not ported. `ROADMAP.md` supersedes `MIGRATE_PLAN.md`, which is frozen as the migration's historical record.
* The domain glossary is not annotated with Go deviations. Every deviation is recorded here, as this document has recorded forty-six of them.
* **Consequences:**
* Every authority citation in the repository resolves locally; the project has no documentation dependency on `cafe-pos`.
* A future session must not treat the TypeScript repository as a specification source, and must not go looking for TypeScript code to port for any remaining phase.
* The nine completed TypeScript effort directories are deliberately not imported. Their acceptance criteria name tRPC, Drizzle, and HeroUI, and the behavior that survived is already carried by the Go specifications, code, and tests. `cafe-pos` remains on disk for anyone needing the original wording.
* The six remaining implementation tickets are renumbered onto Go phases 07 through 12 under `docs/backlog/`, each retaining a `Source:` line. They are marked `ready-for-design` rather than `ready-for-agent` because no approved Go design exists for any of them.
* The nine resolved design questions from the `opening-day-pos` Wayfinder map are copied verbatim to `docs/domain-rationale/`. They are the reasoning that produced `CONTEXT.md`, they hold rules that reached no ticket's acceptance criteria, and ticket 06 is the direct design ancestor of Phases 07 through 09. They are historical reasoning, not authority: `CONTEXT.md` and this document win over them.
* Unresolved design questions that gate launch - receipt and PDF boundary, opening-day readiness, cafe fiscal identity, and the fiscal invoice path - are preserved in `docs/backlog/open-questions.md`. The fiscal invoice path is covered by no phase; if the cafe needs registered e-invoices at opening, a phase must be added.

---

## ADR-048: Authorize current capabilities inside domain transactions

* **Decision Date:** 2026-09-18
* **Status:** Accepted
* **Context:** Adopted from `cafe-pos/docs/adr/0002-authorize-current-capabilities-inside-domain-transactions.md`, the one TypeScript architecture decision that survives migration. Every Go slice already implements this rule, and ADR-009 and ADR-038 record specific applications of it, but no existing record states it as the cross-cutting principle. Closing the migration without adopting it would leave the principle sourceless.
* **Decision:**
* The authenticated request context is not an authority token. Every domain command reloads the acting Staff Identity's current enabled state, roles, and capabilities inside its own transaction, before the idempotency claim and before any replay.
* Manager-only administration and Manager Approval additionally require the current `MANAGER` role and a fresh PIN bound to the exact action.
* Authorization denial returns a decision inside the transaction so its required Audit Event commits without committing an idempotency claim or business effect; the stable domain error is raised after that transaction completes.
* **Consequences:**
* `internal/auth` owns capability derivation, current-authority decisions, fresh-PIN verification, Staff Access Session policy, and security Audit Events behind a transaction-aware interface. Business slices invoke it inside their own single transaction.
* Every idempotent replay rechecks current session, enabled state, Manager role where required, capability, and current PIN. PIN values are excluded from fingerprints, persisted results, Audit Events, logs, and responses.
* Role replacement does not revoke a Staff Access Session, because the next operation reloads current authority. Disabling an identity and resetting its PIN revoke all of its sessions.
* Restated for this stack: the Echo HTTP layer authenticates transport context, validates input, and maps errors, but never becomes the sole authorization enforcement point.
* Verifying a PIN before the business transaction was rejected: identity, role, capability, or PIN state could change before the mutation commits, and any returned proof could become reusable authority.

---

## ADR-049: Blind Shift reconciliation uses a non-abortable CLOSING state

* **Decision Date:** 2026-09-18
* **Status:** Accepted
* **Context:** Phase 07 requires an initial cash count before Expected Cash is revealed. The shipped current-Shift read exposes both Expected Cash and every source term needed to derive it, while leaving a Shift `OPEN` after reveal would allow the reconciled totals to change or let staff return to sales with knowledge of the target.
* **Decision:**
* While a Shift is `OPEN`, its current read exposes metadata but no Opening Float, Cash Movement history, Refund, Expected Cash, or aggregate reconciliation source amount, and Cash Movement mutation responses do not return recomputed Expected Cash. A mutation may echo the individual value its actor just supplied; the control prevents server disclosure of aggregate targets rather than pretending staff forget their own inputs. Starting reconciliation atomically records the blind initial count, freezes the authoritative totals, and changes the Shift to `CLOSING`; only the committed response reveals the snapshot.
* No failed pre-commit reconciliation response exposes Expected Cash, an aggregate source total, or arithmetic operands; guarded calculation failures use a generic stable error.
* `CLOSING` is resumable by any staff member with current `sales_shift.operate`, but it cannot be abandoned back to `OPEN`. At most one `OPEN` or `CLOSING` Shift may exist.
* **Consequences:**
* Current-Shift money visibility is an intentional breaking API change. Blind counting is enforced by the service rather than entrusted to a client.
* An interrupted close does not strand ownership with one login, but normal sales remain suspended until an authorized staff member completes the durable workflow.
* The earlier normal path in which a Service Session outlived its Shift is superseded for new closures; historical cross-Shift attribution remains valid.

---

## ADR-050: Shift closure uses global blockers and the Shift row as its writer gate

* **Decision Date:** 2026-09-18
* **Status:** Accepted
* **Context:** One cashier Shift is active for the cafe. Scoping blockers only to rows carrying its id can miss an active Session or open Check whose financial activity spans Shifts. Closure must also race safely with Session Start and financial writers without reversing their established Check-to-Session-to-Shift lock order.
* **Decision:**
* Reconciliation start and final closure reject global unsettled Checks, pending Refund intents, unresolved financial correction obligations, and active Service Sessions, in that precedence. The correction predicate sums every live adjusted Check's positive valid-receipt excess over corrected charge plus every post-sale adjustment's positive amount not covered by completed Refund allocations, without filtering by Shift attribution.
* Start and close lock the Shift `FOR UPDATE` and perform non-locking blocker reads. Session Start and financial writers whose effects can survive Session closure coordinate through the Shift row; Session Start is brought under this protocol before it inserts a Session. Draft and Commit remain transitively excluded by the global active-Session blocker and their existing Session/Draft locks.
* **Consequences:**
* A writer commits wholly before the frozen snapshot or waits and rejects after the state leaves `OPEN`. Closure never takes Check or Session row locks while holding the Shift, avoiding an inverse lock cycle.
* Financial blockers are reported before the generic active-Session blocker so staff receive the most actionable error.
* Phase 08 may append Awaiting Submission to the precedence but may not weaken these blockers.

---

## ADR-051: Shift closure preserves normalized attempts and three discrepancy dimensions

* **Decision Date:** 2026-09-18
* **Status:** Accepted
* **Context:** Recounts and Manual QR rechecks require durable actor/time evidence, while one net or JSON discrepancy would hide whether Cash, QR received, or QR refunded failed to reconcile. A Manager Approval must authorize the exact final facts rather than a client-provided total.
* **Decision:**
* Cash Counts and Manual QR Observations are normalized append-only ledgers. Final closure references the latest attempt in each ledger and preserves an immutable scalar snapshot.
* Differences are stored separately for `CASH`, `MANUAL_QR_RECEIVED`, and `MANUAL_QR_REFUNDED`, always as observed minus expected. Each nonzero dimension has its own immutable reason; cash-specific and QR-specific reasons may be used only with their matching dimensions, while `UNEXPLAINED` and `OTHER` are shared. One fresh Manager Approval authorizes the complete final discrepancy set.
* A nonzero Cash difference requires a recount. Either nonzero QR difference requires a second full QR observation. No discrepancy creates a balancing financial record.
* **Consequences:**
* The product can explain shortages and excesses without reconstructing overwritten observations or conflating received and refunded bank activity.
* Closure commands bind approval to latest attempt ids and server-derived differences. A later attempt makes a prepared close stale.
* Phase 09 adds post-Shift correction history alongside this snapshot and never rewrites it.

---

## ADR-052: Closed Shift history requires audit.inspect

* **Decision Date:** 2026-09-18
* **Status:** Accepted
* **Context:** Cashiers need the result of the Shift they close, but complete historical reconciliation includes sensitive drawer and bank-observation facts. The role model already grants `audit.inspect` to Manager and not Cashier.
* **Decision:**
* The close mutation returns its immutable summary to the initiating Shift operator. Listing closed Shifts by time range and reading a closed Shift by id require current `audit.inspect` authority.
* **Consequences:**
* No new history capability or role is introduced. Manager can inspect the complete record; Cashier cannot browse historical Shifts after the close response.

---

## ADR-053: Server-authoritative sales state is not mirrored into client stores

* **Decision Date:** 2026-09-21
* **Status:** Accepted
* **Context:** The approved web architecture spec (2026-09-20) allocated `stores/use-pos-store.ts` to hold "cart items, active order, discount" and `stores/use-shift-store.ts` to hold "active shift info". But an Order Draft is not a client cart: it is created, itemized, modified, and committed through `/sales/service-sessions/{id}/draft/*`, and the Sales Shift is likewise server state. A client store holding either would be a second copy of an authoritative fact, free to drift from the server that owns it, with the drift surfacing as a wrong total at the moment money changes hands.
* **Decision:**
* Order Draft, Check, Sales Shift, and Preparation Queue live in the react-query cache and are mutated through the API with optimistic updates. No Zustand store holds them.
* Zustand holds the Staff Access Session and client-local interface state only, such as the open tab or an expanded panel.
* The web architecture spec's `use-pos-store.ts` and `use-shift-store.ts` are superseded and are not created.
* **Consequences:**
* The cashier screen has no local cart to reconcile; a stale draft surfaces as a refetch rather than as a divergent total.
* Every draft edit costs a round trip. On the staff LAN this is the intended trade: correctness of the money path over interaction latency.
* Optimistic update and rollback become a shared concern of the feature `api/` seam rather than of a store.
