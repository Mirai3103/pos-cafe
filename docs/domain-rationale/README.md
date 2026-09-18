# Domain rationale

Nine resolved design questions, copied verbatim from the TypeScript Wayfinder map
`cafe-pos/.scratch/opening-day-pos/issues/`. They are the reasoning that produced
[`CONTEXT.md`](../../CONTEXT.md).

## What these are, and what they are not

`CONTEXT.md` compresses these answers into definitions. These files hold the *why*:
what was considered, what was rejected, and which consequence each rule was chosen to
produce.

**They are not authority.** `CONTEXT.md` is the binding domain authority (ADR-047),
and `spec/decisions.md` records every place Go deviates from it. These are historical
reasoning, resolved before this system existed. Where one of them appears to
contradict `CONTEXT.md` or an ADR, the ADR wins and the contradiction is a finding
worth recording, not a rule to follow.

Read them when designing a phase whose rules they explain — the acceptance criteria
in `docs/backlog/` state *what*, these state *why*. Several carry rules that never
made it into any ticket's criteria.

## What each one covers

| File | Resolved | Explains |
| --- | --- | --- |
| [01 Mixed-service Order lifecycle](01-define-mixed-service-order-lifecycle.md) | Service Session holding Order batches and independently settled Checks | Phases 5A–5D, ADR-016, ADR-040 |
| [02 Menu and Modifier Group rules](02-define-menu-and-modifier-rules.md) | Single-category catalog, explicit Size pricing, immutable historical snapshots | Phase 2 |
| [03 Loyalty extension boundary](03-define-loyalty-extension-boundary.md) | Opening-day sales stay permanently anonymous | Nothing built; guards against a future scope creep |
| [04 TanStack Start on-premise research](04-research-tanstack-start-on-premise.md) | Boot, supervision, addressing, updates, and restore remain explicit appliance decisions | Phases 11–12 — **most stack-dependent; its Node specifics do not survive migration, its appliance conclusions do** |
| [05 Vietnam fiscal obligations research](05-research-vietnam-fiscal-obligations.md) | An internal PDF is not a fiscal plan | The two open fiscal questions in `docs/backlog/open-questions.md` |
| [06 Payments, corrections, shifts, reconciliation](06-define-payments-shifts-and-reconciliation.md) | Commit before Payment; append-only movements; blind-cash closure with preserved discrepancies | **Phases 07, 08, 09** — the direct ancestor of all three |
| [07 Preparation Queue workflow](07-define-preparation-queue.md) | Per-item FIFO queue, explicit handoff states, acknowledged corrections, Remake-only priority | Phases 6A–6C, ADR-028, ADR-037 |
| [08 Staff permissions and audit trail](08-define-staff-permissions-and-audit.md) | Composable capabilities, single-action fresh-PIN approval, append-only audit | Phase 1, ADR-009, ADR-038, ADR-048 |
| [09 Local operation, backup, recovery, updates](09-define-local-operations-and-recovery.md) | Ownership roles, one LAN-authoritative service, bounded recovery | Phases 11–12 |

## Start with 06 for the next phase

Phase 07 (Close and reconcile a Sales Shift) is the work to pick up next, and
[06](06-define-payments-shifts-and-reconciliation.md) is its design ancestor. It
carries load-bearing detail that no ticket's acceptance criteria restate, including:

- The rationale for Expected Cash using applied amount rather than tendered cash:
  "tendered cash less change has the same net effect." This is the reasoning behind
  ADR-046.
- That an open Sales Shift is required before starting a Service Session, submitting
  an Order, receiving a Payment, issuing a Refund, or recording a financial
  correction — while menu administration may occur outside a Shift. Go enforces this
  today through `ErrOpenShiftRequired`.
- That a later exceptional Refund belongs to the currently open Shift while linking
  the Payment and Completed Sale from an earlier one.
- The boundary between Payment Void and Post-Shift Payment Correction, and why staff
  never add balancing Payments, Refunds, or Cash Movements to force agreement.

## Not copied

The six unresolved questions from the same map are triaged in
[`docs/backlog/open-questions.md`](../backlog/open-questions.md) rather than copied.
The nine completed implementation efforts under `cafe-pos/.scratch/` are not copied;
their behavior is carried by the Go specifications, code, and tests (ADR-047).
