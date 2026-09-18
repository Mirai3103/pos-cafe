# Phase 09: Post-Shift corrections and Audit history

**Status:** ready-for-design
**Blocked by:** 07, 08
**Source:** cafe-pos `.scratch/opening-day-pos-v0/issues/14-record-post-shift-corrections-and-audit.md`

**What to build:** Let a Manager append bookkeeping corrections after Shift closure
and inspect the complete business audit trail without rewriting the Shift, Completed
Sale, original Payment, or role-based privacy rules.

## Already present in Go

- **`audit_events` exists and every mutation already writes to it** in the same
  transaction as its business effect. This phase adds a read surface over facts that
  are already being recorded, not a new recording mechanism.
- **Post-sale corrections already exist and are already additive.** Phase 6C
  introduced `POST_SALE` charge adjustments linked to a Completed Sale, and ADR-041
  fixes the rule that a Completed Sale snapshot is never rewritten. That is the same
  principle this phase extends to Payments.
- **Refund already reaches post-sale capacity.** A Manager-approved Refund can
  already allocate against post-sale correction capacity without reopening a Service
  Session (ADR-042).
- **Role-based projection discipline is established.** The Preparation Queue is
  already proven free of financial leakage, and Phase 6C routes already keep
  credentials out of persistence, responses, audits, and logs. The privacy criteria
  below extend an enforced pattern.
- **Single-action Manager Approval** (ADR-009) supplies the approval mechanism.

What remains new: the Post-Shift Payment Correction record itself with its reason
catalog, and the authorized audit-history query surface with its per-role
projections.

## Acceptance criteria

- [ ] A Manager can append a Post-Shift Payment Correction that links the erroneous
      Payment, its replacement when applicable, and any unresolved difference.
- [ ] A Post-Shift Payment Correction does not reopen or rewrite the original Sales
      Shift and does not pretend to move money in the current Shift.
- [ ] Later Refunds and corrections link to the immutable Completed Sale and source
      Payment without reopening the Service Session.
- [ ] Manager Approval and one immutable Post-Shift Payment Correction reason —
      `DUPLICATE_PAYMENT`, `WRONG_AMOUNT`, `WRONG_METHOD`,
      `PAYMENT_RECORDED_IN_ERROR`, or `OTHER` — are required for each correction;
      `OTHER` requires a note.
- [ ] A Manager can inspect complete authorized Audit Events by business identity,
      actor, time, event type, and related correction chain.
- [ ] Cashier and Barista history queries return only the operational and
      current-Shift information allowed by their capabilities.
- [ ] Audit Events remain append-only and preserve actor, target, business meaning,
      reasons, before and after facts, initiator, and approver where applicable.
- [ ] PIN values, QR sender identity, and financial fields forbidden to Barista never
      appear in audit responses for those callers.
- [ ] Concurrent or repeated correction requests cannot duplicate replacement effect
      or mutate a closed snapshot.
- [ ] Integration tests demonstrate correction chains, immutable prior records,
      authorization, filtering, and role-specific projections.
- [ ] Manager history interface demonstrates the same. (Phase 11 — UI)

## Design notes carried from the source

Phase 6C deliberately excluded "Payment Void after Service Session closure, even
when the original Shift remains open," on the grounds that it needs a post-sale
replacement-Payment design. That design belongs here.

The audit read surface is the first query in the system whose result shape depends
on the caller's role rather than only on authorization pass or fail. The
specification must fix whether that is one projection with fields elided or distinct
per-role projections, because a leak here is a privacy defect rather than a bug.

## Read first

[`docs/domain-rationale/06-define-payments-shifts-and-reconciliation.md`](../domain-rationale/06-define-payments-shifts-and-reconciliation.md)
draws the boundary this phase implements: a real current-Shift Payment, Refund, or
Cash Movement affects that Shift's reconciliation, while a bookkeeping-only
post-Shift correction does not. It also places a later exceptional Refund in the
currently open Shift while linking the Payment and Completed Sale from the earlier
one. [`08-define-staff-permissions-and-audit.md`](../domain-rationale/08-define-staff-permissions-and-audit.md)
covers the audit trail and per-role visibility.

## Open questions

None blocking.
