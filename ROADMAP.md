# POS Cafe Roadmap

Phases 0 through 6C are complete. The TypeScript-to-Go migration is **closed**
(ADR-047): this repository has passed its source, and every phase from 07 onward is
designed from [`CONTEXT.md`](CONTEXT.md) rather than ported from TypeScript code.

Phase **08** is unblocked and is the work to pick up next.

---

## Where the authority lives

| Document | Job |
| --- | --- |
| [`CONTEXT.md`](CONTEXT.md) | Domain language. What a term means in the business. Binding. |
| [`spec/decisions.md`](spec/decisions.md) | Architecture decisions, including every deviation from canonical behavior. Append-only. |
| [`docs/superpowers/specs/`](docs/superpowers/specs/) | Per-phase approved designs. Frozen once approved. |
| [`docs/backlog/`](docs/backlog/) | Work not yet designed. Superseded by a spec when its phase begins. |
| [`docs/domain-rationale/`](docs/domain-rationale/) | Why `CONTEXT.md` says what it says. Historical reasoning, not authority. |
| `ROADMAP.md` | This file. Current status. The only one updated as work lands. |
| [`MIGRATE_PLAN.md`](MIGRATE_PLAN.md) | Historical record of the migration. Frozen. |

When `CONTEXT.md` and an implementation disagree, `CONTEXT.md` states the intent and
`spec/decisions.md` states why Go differs. An unrecorded divergence is a defect.

---

## Delivered

| Phase | Scope | Design |
| :--- | :--- | :--- |
| **0** | Echo, pgx, embedded migrations, sqlc, Watermill, validation, error envelope, Swagger | — |
| **1** | Auth and staff management | [spec](docs/superpowers/specs/2026-09-09-auth-slice-design.md) |
| **2** | Catalog, menu projections | [spec](docs/superpowers/specs/2026-09-10-catalog-slice-design.md) |
| **3** | Tables and floor layout | [spec](docs/superpowers/specs/2026-09-12-tables-slice-design.md) |
| **4** | Sales Shift, Cash Movements, Expected Cash | [spec](docs/superpowers/specs/2026-09-13-shift-slice-design.md) |
| **5A** | Service Session, table assignment, Order Draft | [spec](docs/superpowers/specs/2026-09-13-sales-session-draft-design.md) |
| **5B** | Commit, Committed Items, Checks, Charge Allocations | [spec](docs/superpowers/specs/2026-09-14-sales-commit-checks-design.md) |
| **5C** | Cash and Manual QR Payments, check split and merge, settlement | [spec](docs/superpowers/specs/2026-09-14-sales-payments-settlement-design.md) |
| **5D** | Submit, Orders, Preparation Units, Session closure, Completed Sale | [spec](docs/superpowers/specs/2026-09-15-sales-submission-closure-design.md) |
| **6A** | Preparation Queue reads and bulk transitions | [spec](docs/superpowers/specs/2026-09-16-preparation-queue-transitions-design.md) |
| **6B** | Alerts, Waste, Remake, priority, state correction | [spec](docs/superpowers/specs/2026-09-17-preparation-corrections-design.md) |
| **6C** | Cancellation, Comp, Refund, Payment Void, Shift reconciliation terms | [spec](docs/superpowers/specs/2026-09-18-preparation-financial-corrections-design.md) |
| **07** | Close and reconcile a Sales Shift | [spec](docs/superpowers/specs/2026-09-18-shift-closure-reconciliation-design.md) |

Supporting: [PostgreSQL template integration tests](docs/superpowers/specs/2026-09-15-postgres-template-integration-tests-design.md),
[roadmap migration and closure](docs/superpowers/specs/2026-09-18-roadmap-migration-design.md).

Six vertical slices ship: `auth`, `catalog`, `tables`, `shift`, `sales`,
`preparation`. Fifty-two architecture decisions are recorded.

---

## Remaining

Every phase below is `ready-for-design`: no approved Go design exists for any of
them, so each begins with brainstorming rather than implementation. Acceptance
criteria live in the ticket, not here.

| Phase | Scope | Blocked by | Ticket |
| :--- | :--- | :--- | :--- |
| **08** | Recover a failed or abandoned checkout | none | [ticket](docs/backlog/phase-08-recover-failed-or-abandoned-checkout.md) |
| **09** | Post-Shift corrections and Audit history | 07, 08 | [ticket](docs/backlog/phase-09-post-shift-corrections-and-audit.md) |
| **10** | Recover numbered-paper outage Sales | 09 | [ticket](docs/backlog/phase-10-recover-numbered-paper-outage-sales.md) |
| **11** | Serve clients over LAN, single-binary packaging, frontend | none | [ticket](docs/backlog/phase-11-clients-over-lan-and-single-binary.md) |
| **12** | Back up, restore, update, verify readiness | 10, 11 | [ticket](docs/backlog/phase-12-backup-restore-update-readiness.md) |

```
07 ─┐
    ├─> 09 ──> 10 ──┐
08 ─┘              ├─> 12
11 ────────────────┘
```

Phase 11 is unblocked but order-sensitive: its packaging and LAN half can be proven
at any time, while its generated TypeScript client goes stale if produced before
Phases 07 through 10 add their routes.

Design questions that gate this work but are not themselves implementable — receipt
boundary, opening-day readiness, fiscal identity, fiscal invoice path — live in
[`docs/backlog/open-questions.md`](docs/backlog/open-questions.md). The fiscal
invoice path is not covered by any phase above; if the cafe needs registered
e-invoices at opening, a phase must be added.

---

## Conventions

Money is whole VND stored as `BIGINT`. Every mutation reloads current authority
inside its own transaction, carries a request identifier, is idempotent, and persists
its Audit Event in the same transaction (ADR-048). Financial and commercial facts are
append-only; corrections are new linked records, never edits. Slices do not import
each other's packages — ADR-024 fixes the Sales/Preparation boundary and ADR-040
records its one deliberate exception.
