# Open questions

Design questions that are not implementable as they stand. They come from the
TypeScript Wayfinder map `cafe-pos/.scratch/opening-day-pos/`, where the nine
resolved questions became `CONTEXT.md` and the specifications this system was built
from. These are the ones that were never resolved.

Each entry records what is being asked, why it still matters here, who can answer it,
and what it blocks. None of them blocks Phase 07 or Phase 08.

---

## Receipt and PDF boundary

**Source:** `opening-day-pos/issues/10-define-receipt-and-pdf-boundary.md` (grilling,
blocked by the fiscal-invoice path)

What information and lifecycle make a customer receipt correct; when may it be
generated or regenerated; how does an opening-day PDF relate to fiscal documents; and
what stable boundary should later receipt-printer adapters consume without putting
device concepts inside the Order or Payment model?

**Why it still matters.** No Go slice produces a customer receipt. Phase 6C lists
receipt generation as an explicit non-goal, and the Phase 5 architecture mapping in
`MIGRATE_PLAN.md` named "Receipt" as in-scope for Sales without it ever being
designed. The cafe currently has no defined answer to what it hands a customer.

**Who answers:** design question, resolvable here — but it depends on the fiscal
invoice path below, because a receipt that is not a fiscal document must not look
like one.

**Blocks:** Phase 10 (a recovered paper sale may need the same document), Phase 11
(whether the cashier station renders or prints anything).

---

## Opening-day readiness

**Source:** `opening-day-pos/issues/12-define-opening-day-readiness.md` (grilling)

What observable acceptance criteria, menu-loading steps, staff rehearsal scenarios,
backup-restore drill, manual fallback drill, data setup, and go/no-go checks prove
the Core POS can become the cafe's sole sales system? Define the readiness gate and
its schedule relative to the opening date; do not execute the rollout.

**Why it still matters.** Phase 12 has a criterion requiring a Daily POS Readiness
Check, and that check cannot be specified until this is settled.

**Who answers:** the POS Operations Owner, with engineering input. The schedule half
needs a real opening date.

**Blocks:** Phase 12.

---

## Cafe fiscal identity

**Source:** `opening-day-pos/issues/13-confirm-cafe-fiscal-identity.md` (task)

Establish, with a Vietnamese accountant or tax professional and where needed the
directly managing tax authority, the seller's legal form, credible annual-revenue
branch, VAT and accounting method, applicable invoice registration, managing tax
authority, and required retention schedule.

**Why it still matters.** It is recorded here specifically so it is not mistaken for
engineering work. The source ticket states the software project must not infer these
facts.

The source ticket carries a note from 2026-08-28: the owner deferred it because they
needed a runnable product first, and it was returned to open. That deferral is still
in force and is reasonable — but the same note is explicit that fiscal identity must
be confirmed before the product is treated as the cafe's opening-day sole sales
system. This repository is now close enough to that point for the deferral to be
worth revisiting.

**Who answers:** the cafe owner, with an accountant or tax professional. Not
answerable from the codebase.

**Blocks:** the fiscal invoice path, and transitively the receipt boundary.

---

## Fiscal invoice path

**Source:** `opening-day-pos/issues/14-choose-opening-day-fiscal-invoice-path.md`
(grilling, blocked by fiscal identity)

Which registered invoice type and provider will the cafe use; which event triggers
invoices for takeaway, prepaid dine-in, and pay-later dine-in; how will credentials
and buyer retrieval be owned; and what durable pending, submitted, accepted,
rejected, corrected, outage, retry, reconciliation, and retention behavior must the
Core POS support?

**Why it still matters.** This is the largest unbuilt subsystem in the product, and
it is not on the roadmap at all — no phase from 07 to 12 covers it. If the cafe needs
registered e-invoices at opening, a phase must be added. The source research
concluded that an internal PDF is not a fiscal plan.

**Who answers:** the owner chooses the provider; the replaceable provider boundary is
an engineering design question that should be settled before receipt rendering.

**Blocks:** the receipt boundary. Potentially adds a phase.

---

# Closed on arrival

## Prototype counter and table workflows

**Source:** `opening-day-pos/issues/11-prototype-counter-and-table-workflows.md`
(prototype)

**Disposition: deferred to Phase 11.** The question is about client workflow, and
Phase 11 owns the frontend. It is not resolved, but it is no longer loose: it is part
of that phase's scope.

## Define durable sales model and module seams

**Source:** `opening-day-pos/issues/15-define-durable-sales-model-and-module-seams.md`
(grilling)

**Disposition: resolved.** The question asked what the durable sales model and module
boundaries should be. This system answered it by building them: ADR-001 through
ADR-046 record the decisions, and the six vertical slices under `internal/` are the
seams. ADR-024 fixes the Sales/Preparation boundary and ADR-040 records the one
deliberate exception to it. Carrying this forward as an open question would
misrepresent the state of the system.
