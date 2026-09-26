# Backlog

Work that is not yet designed. Each file here is one phase of `ROADMAP.md`.

A backlog ticket states *what must become true*, not how. It carries no schema, no
query, and no Go interface. The moment a phase starts, it goes through brainstorming
and produces an approved design specification under
`docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`; from then on that
specification is the authority and this ticket is history.

## Authority

These tickets originate in the TypeScript project `cafe-pos`, whose tracker lived at
`.scratch/opening-day-pos-v0/issues/`. That project is no longer a specification
source — see ADR-047. Each ticket keeps a `Source:` line so the original wording
stays recoverable, but the binding domain authority is `CONTEXT.md` in this
repository, and every recorded deviation lives in `spec/decisions.md`.

The acceptance criteria below were written against a codebase that had none of this
system's behavior. Each ticket therefore carries an **Already present in Go**
section naming what existing slices, migrations, and ADRs already satisfy. Read that
section before estimating: several criteria are already met.

Why a rule exists, as opposed to what it requires, is in
[`docs/domain-rationale/`](../domain-rationale/) — the nine resolved design questions
that produced `CONTEXT.md`. Several carry load-bearing detail that reached no ticket's
acceptance criteria. Each ticket below names the ones worth reading first.

## Status

| Status | Meaning |
| --- | --- |
| `ready-for-design` | No approved specification exists. The phase begins with brainstorming, not implementation. |
| `in-design` | A specification is being written or reviewed. |
| `in-progress` | An approved specification and plan exist; implementation is running. |
| `completed` | Every acceptance criterion is satisfied and `ROADMAP.md` shows the phase as done. |
| `deferred` | Deliberately not scheduled. The entry records why. |

A phase is unblocked when every phase named in its `Blocked by` line is `completed`.
`deferred` never satisfies a dependency.

`ready-for-design` is deliberate. The TypeScript tracker used `ready-for-agent`,
which there meant an agent could implement directly from an approved spec. No Go
design exists for any phase below, so none of them can be picked up and built.

## Shared contract

Every phase inherits the conventions the system already enforces, so its ticket does
not restate them:

- New durable records use stable non-sequential public identities and
  server-authoritative instants rendered in `Asia/Ho_Chi_Minh`.
- Every mutation reloads current authority inside its own transaction, carries a
  request identifier, is idempotent, and persists its required Audit Event in the
  same transaction (ADR-048).
- Money is whole VND stored as `BIGINT`.
- Financial and commercial facts are append-only. Corrections are new linked
  records, never edits.

## Criteria marked `(Phase 11 — UI)`

Criteria naming screens, Vietnamese labels, or client behavior are kept and marked
rather than deleted. The current effort excludes the frontend; it does not abandon
it. Phase 11 owns them.

## Files

| Phase | Ticket | Status | Blocked by |
| --- | --- | --- | --- |
| 07 | [Close and reconcile a Sales Shift](phase-07-close-and-reconcile-sales-shift.md) | completed | none |
| 08 | [Recover a failed or abandoned checkout](phase-08-recover-failed-or-abandoned-checkout.md) | ready-for-design | none |
| 09 | [Post-Shift corrections and Audit history](phase-09-post-shift-corrections-and-audit.md) | ready-for-design | 07, 08 |
| 10 | [Recover numbered-paper outage Sales](phase-10-recover-numbered-paper-outage-sales.md) | ready-for-design | 09 |
| 11 | [Serve clients over LAN and single-binary packaging](phase-11-clients-over-lan-and-single-binary.md) | ready-for-design | none |
| 12 | [Back up, restore, update, and verify readiness](phase-12-backup-restore-update-readiness.md) | ready-for-design | 10, 11 |

Design questions that gate this work but are not themselves implementable live in
[open-questions.md](open-questions.md).

Follow-ups outside the phase sequence:

| Ticket | Status | Blocked by |
| --- | --- | --- |
| [Backend alignment: close the gaps the web slices recorded](backend-alignment.md) | in-progress (BA-1 done) | none |
| [Catalog fields the availability cards mock today](availability-card-fields.md) | completed (BA-1) | none |
