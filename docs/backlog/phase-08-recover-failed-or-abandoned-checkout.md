# Phase 08: Recover a failed or abandoned checkout

**Status:** ready-for-design
**Blocked by:** none
**Source:** cafe-pos `.scratch/opening-day-pos-v0/issues/13-recover-failed-or-abandoned-checkout.md`

**What to build:** Make interrupted checkout outcomes explicit so staff can retry
paid work safely, cancel and return money, or abandon unpaid work without duplicate
charges, hidden Orders, or false Completed Sales.

## Why this is unblocked

Like Phase 07, the source ticket is blocked by TypeScript ticket 11, which Go
delivered as Phase 6C. The Refund workflow this phase leans on already exists.

Phase 6C listed "Abandoned Checkout and the paid Awaiting Submission recovery flow"
as an explicit non-goal, deferring it here.

## Already present in Go

- **Submit is synchronous and is the queue-creation boundary** (ADR-033). Preparation
  Units are created inside the Submit transaction, not through Watermill. That makes
  "Submit failed after Payment landed" a real, reachable state rather than an
  eventual-consistency artifact — which is exactly why this phase is needed.
- **Idempotency is already shared and atomic** (ADR-007). Commit, Payment, Submit,
  and the 6C correction commands all claim an idempotency key inside their own
  transaction, so exact replay already returns the original result. Retry-safety for
  the existing commands is not new work; what is new is a durable name for the state
  between a successful Payment and a failed Submit.
- **Refund exists with both methods** (ADR-042, ADR-043). Cash completes in the
  recording transaction; Manual QR stays pending until explicit confirmation. The
  cancel-and-refund path can use it as-is.
- **Closure blockers have an established shape.** Service Session closure already
  rejects pending Refund through `PENDING_REFUND_FOR_CLOSURE` with documented
  precedence (ADR-044). The Awaiting Submission blocker should extend that ordering
  rather than introduce a parallel mechanism.
- **Committed Items are already immutable snapshots** and already survive
  restructuring, so preserving them through abandonment needs no schema change to
  them.

What remains new: the Awaiting Submission state itself and its persistence, the
Abandoned Checkout terminal record with its reason catalog, and the two new closure
blockers.

## Acceptance criteria

- [ ] Paid Committed Items that cannot complete Submit persist visibly as Awaiting
      Submission with their original Check, Payment, snapshot, actor, and failure
      meaning.
- [ ] Retrying Submit uses the original committed facts and idempotency identity and
      can never create a second Order or Preparation Unit for the same work.
- [ ] Staff can cancel Awaiting Submission and complete the required linked Refund
      without silently submitting preparation work.
- [ ] A Service Session and Sales Shift cannot close while Awaiting Submission or
      its required Refund remains unresolved.
- [ ] An unpaid unsubmitted checkout can terminate as an Abandoned Checkout with
      actor, time, one of `CUSTOMER_LEFT`, `CUSTOMER_REQUEST`, `SYSTEM_FAILURE`, or
      `OTHER`, and the note required by `OTHER`.
- [ ] A checkout with any Payment cannot be abandoned until every Payment has been
      fully returned through the normal Refund workflow.
- [ ] Abandonment creates no Order, Preparation Unit, or Completed Sale, while its
      immutable Committed Items and Audit Events remain inspectable.
- [ ] Network retries of Commit, Payment, Submit, Cancel, Refund, and Abandon return
      stable results and do not duplicate money or work.
- [ ] Integration tests exercise induced Submit failure, successful retry,
      cancel-and-refund, unpaid abandonment, paid rejection, closure blockers, and
      concurrency.
- [ ] The Cashier interface keeps Awaiting Submission and abandoned outcomes
      prominent and gives concrete Vietnamese retry or resolution actions.
      (Phase 11 — UI)

## Design notes carried from the source

Inducing a Submit failure is itself a design problem: the existing Submit is one
PostgreSQL transaction, so a failure that leaves Payment durable and Submit undone
must be a failure *between* commits, not inside one. The specification must say
precisely which failure boundary produces Awaiting Submission, and the test suite
must be able to induce it deterministically rather than by timing.

If Phase 07 lands first, this phase adds its blocker to the Shift closure precedence
list Phase 07 establishes. If this phase lands first, Phase 07 inherits the blocker.
Either order works; neither may leave the precedence undocumented.

## Open questions

None blocking.
