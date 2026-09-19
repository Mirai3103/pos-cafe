# Phase 07: Close and reconcile a Sales Shift

**Status:** completed
**Blocked by:** none
**Source:** cafe-pos `.scratch/opening-day-pos-v0/issues/12-close-and-reconcile-sales-shift.md`

**What to build:** Give closing staff an honest, immutable Shift reconciliation:
resolve outstanding work, count cash blindly, compare Manual QR totals, preserve
discrepancies, and close with Manager acknowledgment when necessary.

## Why this is unblocked

The source ticket is blocked by TypeScript ticket 11, "Correct charges and Payments
without mutation." That ticket is still `ready-for-agent` in `cafe-pos`, but Go
delivered the same scope as Phase 6C: Comp, Refund, Payment Void, and the corrected
Check equations all exist here. The dependency is satisfied, and this is the first
phase to pick up.

It is also the phase that closes the system's largest operational gap. `internal/shift`
exposes Open Shift, the current read, and Cash Movement, but no Close. A Sales Shift
that has been opened therefore stays `OPEN` permanently and a second one can never
be opened — recorded as an accepted consequence in the Phase 4 specification, on the
understanding that Phase 5 and the correction work would come first. They have.

## Already present in Go

- `internal/shift` — the slice, its `Runner`, capability `sales_shift.operate`,
  advisory-locked single-open-Shift invariant, and idempotent mutation execution.
- **Expected Cash is already derived, not stored.** ADR-046 fixes the formula as
  Opening Float + non-voided Cash Payments − completed Cash Refunds + Pay Ins −
  Pay Outs, and `ComputeExpectedCash` in `internal/shift/current.go` implements it.
- **Payment Void effects already participate.** ADR-045 makes a Void append-only and
  restricted to a still-open original Shift, and ADR-046 removes its source term.
- **Manual QR net received** is already derived as
  `manual_qr_payment_vnd - manual_qr_payment_void_vnd`.
- **Pending Refund and unresolved post-sale correction amounts are already
  reported** by the current-Shift read. ADR-046 states explicitly that a later Shift
  Close design must consume these authoritative fields rather than inventing another
  calculation. This ticket is that design.
- **Single-action Manager Approval** exists as a reusable mechanism (ADR-009), used
  by Cash Movement, Comp, Refund, and Payment Void.
- **A closure-blocker precedent exists.** Service Session closure already rejects a
  pending Refund through `PENDING_REFUND_FOR_CLOSURE` with a documented precedence
  order (ADR-044). Shift closure should follow that shape rather than invent one.

What remains is genuinely new: the blind cash count, observed Manual QR entry,
Shift Discrepancy with its reason catalog and approval, and the immutable closure
snapshot.

## Acceptance criteria

- [x] Normal Shift closure is blocked by any active Service Session, unsettled
      Check, pending Refund, or unresolved financial correction.
- [x] Closing staff enter an initial blind cash count before Expected Cash is
      revealed; recounts retain initial and final actor and time.
- [x] Expected Cash is derived from Opening Float plus Cash Payments and Pay Ins
      minus Cash Refunds and Pay Outs, including valid Payment Void effects.
- [x] Expected Manual QR receipts and refunds are shown separately after the blind
      cash step.
- [x] Closing staff enter observed Manual QR received and refunded totals, including
      explicit zeroes, without storing screenshots or sender identity.
- [x] A nonzero cash or QR Shift Discrepancy triggers recount or recheck and
      requires `CASH_COUNT_DIFFERENCE`, `QR_OBSERVATION_DIFFERENCE`, `UNEXPLAINED`,
      or `OTHER` plus single-action Manager Approval before closure.
- [x] An approved discrepancy remains visible; the product never creates balancing
      Payments, Refunds, or Cash Movements automatically.
- [x] Closure atomically freezes opener, closer, times, Opening Float, method
      totals, correction totals, Expected and observed values, discrepancies,
      reasons, and acknowledgers.
- [x] A closed Sales Shift cannot reopen or accept current-Shift actions and remains
      inspectable by authorized staff.
- [x] Opening a new Sales Shift after closure succeeds, restoring the operating
      cycle the Phase 4 specification deferred.
- [x] Integration tests cover exact closure, every blocker, recount, QR comparison,
      discrepancy approval, immutable snapshots, and concurrent closure attempts.
- [ ] Basic UI covers the blind-count step, the reveal, discrepancy approval, and
      the closed-Shift summary. (Phase 11 — UI; deferred, the UI phase owns it)

## Design notes carried from the source

The source criterion for closure blockers also names "Awaiting Submission state,"
which Phase 08 introduces. Phase 07 should implement the blockers for states that
exist and leave Phase 08 to add its own, the way Phase 6C added
`PENDING_REFUND_FOR_CLOSURE` to Service Session closure rather than pre-building a
blocker for something unbuilt. The precedence order between blockers must be
documented, as ADR-044 documented it for Session closure.

Blind counting is a control, not a UI preference: Expected Cash must not be readable
by the closing actor before the initial count is durably recorded. Where that
sequencing is enforced — one command in two steps, or two commands sharing a
closure draft — is the central design question of this phase.

## Read first

[`docs/domain-rationale/06-define-payments-shifts-and-reconciliation.md`](../domain-rationale/06-define-payments-shifts-and-reconciliation.md)
is this phase's design ancestor. It carries reasoning the criteria above do not
restate: why Expected Cash uses applied amount rather than tendered cash, the
system-wide rule that an open Shift precedes every Session, Order, Payment, Refund,
and correction, where a later exceptional Refund belongs, and why staff never add a
balancing transaction to force agreement.

## Open questions

None blocking. [Opening-day readiness](open-questions.md#opening-day-readiness)
depends on this phase, not the other way around.
