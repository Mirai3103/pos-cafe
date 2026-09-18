# Design Specification: Roadmap Migration And Migration Closure

- **Author:** Claude Opus 5 & Team
- **Date:** 2026-09-18
- **Status:** Approved
- **Phase:** Cross-cutting, documentation authority

---

## 1. Purpose

This specification defines how the surviving planning documents of the TypeScript project `cafe-pos` move into `pos-cafe`, and how the TypeScript-to-Go migration is formally closed.

`pos-cafe` has passed its source. The TypeScript tracker `.scratch/opening-day-pos-v0/` holds seventeen implementation tickets; nine are `completed` there. Tickets 07 (Split Checks and mixed settlement) and 11 (Correct charges and Payments without mutation) remain `ready-for-agent` in TypeScript and are already delivered in Go as Phase 5C and Phase 6C. From ticket 12 onward no canonical implementation exists on either side: the remaining work is designed from `CONTEXT.md`, not ported from code.

Two consequences follow. First, "migration" is no longer an accurate frame for the work ahead, and `MIGRATE_PLAN.md` is no longer an accurate roadmap. Second, the Go specifications cite `cafe-pos/CONTEXT.md` as binding authority in fifteen places, and `spec/decisions.md` cites a `cafe-pos/.scratch` ticket. Those are dangling references to a repository this project does not own and does not track. The migration cannot be called closed while its authority lives elsewhere.

### Goals

1. Bring the domain glossary into this repository so every authority citation resolves inside `pos-cafe`.
2. Preserve the remaining roadmap as actionable backlog tickets renumbered onto Go phases, with source traceability retained.
3. Preserve the unanswered design questions that still gate launch, separated from the ones Go has already answered.
4. Record migration closure as an architecture decision, so that a future session does not go looking for TypeScript code to port.
5. Leave `MIGRATE_PLAN.md` intact as a historical record rather than rewriting it.

### Non-Goals

- Implementing any backlog ticket. This specification moves and restructures documents; it writes no Go code, no SQL, and no tests.
- Rewriting the approved Phase 0 through 6C design specifications. Their prose is an approved historical record. Only one mechanical path substitution is applied to them, defined in Section 5.
- Importing the nine completed TypeScript efforts (approximately 120 files). Their business behavior is already carried by the Go specifications, the Go code, and the Go test suites. Retaining them would create a second, competing source of truth whose acceptance criteria name tRPC, Drizzle, and HeroUI.
- Importing `docs/agents/backend-modules.md` and `docs/agents/domain.md`. Both describe TypeScript module structure that `AGENTS.md` and the vertical-slice layout already supersede.
- Importing `docs/adr/0001-deepen-sales-module-behind-trpc.md`, which is specific to a transport this project does not use.
- Frontend work. Ticket criteria that belong to the user interface are marked and deferred, not deleted.
- Any change to `.scratch/` inside `cafe-pos`. That repository is read-only here.

---

## 2. Source Inventory

The TypeScript project at `E:/Code/cafe-pos` holds these planning documents.

| Source | Size | Disposition |
| --- | --- | --- |
| `CONTEXT.md` | 301 lines | Copied verbatim to `pos-cafe/CONTEXT.md` |
| `.scratch/opening-day-pos-v0/issues/12..17` | 6 tickets | Rewritten as `docs/backlog/phase-07..12` |
| `.scratch/opening-day-pos/issues/10,12,13,14` | 4 open fog tickets | Summarized into `docs/backlog/open-questions.md` |
| `.scratch/opening-day-pos/issues/11` | 1 open fog ticket | Recorded as deferred to Phase 11 |
| `.scratch/opening-day-pos/issues/15` | 1 open fog ticket | Recorded as resolved by existing Go ADRs |
| `docs/adr/0002-authorize-current-capabilities-inside-domain-transactions.md` | 17 lines | Adopted as Go ADR-048 |
| `docs/agents/issue-tracker.md`, `triage-labels.md` | 55 lines | Condensed into `docs/backlog/README.md` |
| `.scratch/opening-day-pos-v0/issues/01..11` | 11 tickets | Not copied; delivered as Phase 0 through 6C |
| 9 completed effort directories | ~120 files | Not copied |
| `docs/agents/backend-modules.md`, `domain.md` | 110 lines | Not copied |
| `docs/adr/0001` | 12 lines | Not copied |

---

## 3. Target Structure

```
pos-cafe/
├─ CONTEXT.md                  (new, verbatim copy)
├─ ROADMAP.md                  (new)
├─ MIGRATE_PLAN.md             (closure banner prepended; body unchanged)
├─ spec/decisions.md           (ADR-047 and ADR-048 appended)
└─ docs/backlog/
   ├─ README.md
   ├─ phase-07-close-and-reconcile-sales-shift.md
   ├─ phase-08-recover-failed-or-abandoned-checkout.md
   ├─ phase-09-post-shift-corrections-and-audit.md
   ├─ phase-10-recover-numbered-paper-outage-sales.md
   ├─ phase-11-clients-over-lan-and-single-binary.md
   ├─ phase-12-backup-restore-update-readiness.md
   └─ open-questions.md
```

### 3.1 Document Authority After This Change

The repository carries several kinds of planning document, and each has exactly one job. A reader who needs to know which document wins consults this table.

| Document | Job | Mutability |
| --- | --- | --- |
| `CONTEXT.md` | Domain language. Defines what a term means in the business. | Stable; changes only when the business changes |
| `spec/decisions.md` | Architecture decisions, including every deviation from canonical behavior | Append-only |
| `docs/superpowers/specs/` | Per-phase approved designs | Frozen once approved |
| `docs/backlog/` | Work not yet designed | Status transitions; superseded by a spec when a phase begins |
| `ROADMAP.md` | Current status of every phase, and pointers | Updated as phases land |
| `MIGRATE_PLAN.md` | Historical record of the TypeScript-to-Go migration | Frozen |

When `CONTEXT.md` and a Go implementation disagree, `CONTEXT.md` states the intent and `spec/decisions.md` states why Go differs. Neither silently overrides the other: an unrecorded divergence is a defect.

---

## 4. CONTEXT.md

`CONTEXT.md` is copied byte-for-byte to the repository root.

The glossary describes the cafe's business domain, not the TypeScript implementation of it. Terms such as Sales Shift, Expected Cash, Preparation Unit, Outage Sale Record, and Recovery Custody Pack are equally true of the Go system. Copying it verbatim therefore preserves its meaning while making every existing citation resolve locally.

The glossary is **not** annotated with Go deviations. `spec/decisions.md` already holds forty-six such records and is the established place for them. Annotating the glossary inline would require editing it at the end of every phase and would mix a stable document with an append-only one.

The glossary defines terms this system has not yet built: Shift Reconciliation, Shift Discrepancy, Post-Shift Payment Correction, Outage Recovery Entry, Daily POS Readiness Check. That is correct and intended. The glossary describes the target domain, and `ROADMAP.md` plus `docs/backlog/` record which parts of it exist today.

---

## 5. Reference Rewriting

Two classes of reference to `cafe-pos` exist in the current documents, and they are treated differently.

**Live authority citations.** Fifteen occurrences of the path `cafe-pos/CONTEXT.md` appear across `docs/superpowers/specs/*.md` and `spec/decisions.md`. Each is a citation of binding authority, so each must resolve. Every occurrence is rewritten mechanically to `CONTEXT.md`. Only the path token changes; no surrounding prose is touched.

**Historical provenance.** Seven occurrences name TypeScript source paths (`cafe-pos/src/sales`, `cafe-pos/src/tables`, `cafe-pos/src/sales-shift`, `cafe-pos/src/preparation`) and one names `cafe-pos/.scratch/opening-day-pos/issues/01`. These record where a behavior was observed during migration. They are left exactly as written. ADR-047 states that such references are provenance and carry no live authority, which preserves the approved specifications unedited while removing the ambiguity.

The distinction matters because the first class is a broken dependency and the second class is a citation of history. Rewriting the second class would falsify the record; leaving the first class would leave the migration unclosable.

---

## 6. Backlog Tickets

### 6.1 Renumbering

The six remaining implementation tickets are renumbered onto Go phase numbers. Migration is closing, so Go numbering becomes the only numbering. Each ticket file carries a `Source:` line naming its TypeScript origin so the mapping stays recoverable.

| Go phase | TypeScript ticket | Blocked by | Rewrite depth |
| --- | --- | --- | --- |
| 07 Close and reconcile a Sales Shift | 12 | none | Near-verbatim |
| 08 Recover a failed or abandoned checkout | 13 | none | Near-verbatim |
| 09 Post-Shift corrections and Audit history | 14 | 07, 08 | Near-verbatim |
| 10 Recover numbered-paper outage Sales | 15 | 09 | Near-verbatim |
| 11 Serve clients over LAN and single-binary packaging | 16, plus `MIGRATE_PLAN.md` Phase 7 | none | Rewritten |
| 12 Back up, restore, update, and verify readiness | 17 | 10, 11 | Partly rewritten |

Phases 07, 08, and 11 are unblocked. Phases 07 and 08 depend in TypeScript on ticket 11, which is still `ready-for-agent` there; Go delivered that work as Phase 6C, so the dependency is already satisfied here. This is the single most consequential finding of the source review and is stated explicitly in each of those two tickets.

Phase 11 absorbs the existing `MIGRATE_PLAN.md` Phase 7 (OpenAPI client generation, static asset embedding, hardware verification) together with TypeScript ticket 16 (LAN binding, health endpoint, Local Access QR, production smoke test). Both describe the same deliverable: one authoritative local service reachable by the cafe's clients. Keeping them apart would split one phase across two trackers.

Phase 11 carries no `Blocked by` edge because its source ticket depends only on work already delivered. Its packaging and LAN half is independent of every remaining phase. Its generated-client half is not blocked but is order-sensitive: each of Phases 07 through 10 adds routes, so a client generated before them goes stale. The ticket records that as sequencing guidance, not as a dependency, so that packaging can be proven early on low-spec hardware.

### 6.2 Ticket Structure

Each ticket file uses this structure, adapted from the TypeScript convention so that `Status` and `Blocked by` keep their existing semantics:

```markdown
# Phase NN: <title>

**Status:** ready-for-design
**Blocked by:** <phase numbers, or "none">
**Source:** cafe-pos `.scratch/opening-day-pos-v0/issues/NN-<slug>.md`

**What to build:** <one paragraph, carried from the source ticket>

## Already present in Go
<what existing slices, migrations, and ADRs already satisfy>

## Acceptance criteria
- [ ] <criterion>
- [ ] <criterion> (Phase 11 — UI)

## Open questions
<links into open-questions.md where the ticket depends on unresolved fog>
```

Three additions distinguish these from their sources.

**`Already present in Go`.** The sources were written against a codebase that had none of this. Phase 07, for example, inherits Expected Cash derivation, Cash Movement recording, single-action Manager Approval, and the Refund and Payment Void terms added by ADR-046, leaving blind cash count, Manual QR observation, discrepancy approval, and the immutable closure snapshot as the actual remaining work. Without this section a reader would re-derive work that exists.

**UI marking.** Criteria naming screens, Vietnamese labels, or client behavior are suffixed `(Phase 11 — UI)` rather than deleted. The current effort excludes the frontend; it does not abandon it.

**`Status: ready-for-design`.** The TypeScript vocabulary is `ready-for-agent`, which in that repository meant an agent could begin implementing from an approved spec. No Go design specification exists for any of these phases, so the honest state is that each awaits brainstorming first. `docs/backlog/README.md` defines the vocabulary.

### 6.3 Status Vocabulary

`docs/backlog/README.md` defines five states and one dependency rule, condensed from the TypeScript tracker conventions:

- `ready-for-design` — no approved specification exists; the phase begins with brainstorming.
- `in-design` — a specification is being written or reviewed.
- `in-progress` — an approved specification and plan exist and implementation is running.
- `completed` — every acceptance criterion is satisfied and the phase appears as done in `ROADMAP.md`.
- `deferred` — deliberately not scheduled; the entry records why.

A phase is unblocked when every phase named in its `Blocked by` line is `completed`. `deferred` never satisfies a dependency.

---

## 7. Open Questions

`docs/backlog/open-questions.md` carries the design questions that remain genuinely unanswered, each with its source, why it still matters to the Go system, and who can answer it. Four are carried forward:

- **Receipt and PDF boundary.** No Go slice produces a customer receipt. The Phase 6C specification lists receipt generation as a non-goal. The question of what the cafe hands a customer is unanswered and blocks nothing yet, but gates launch.
- **Opening-day readiness.** Phase 12 cannot define its Daily POS Readiness Check until this is settled.
- **Cafe fiscal identity.** A business decision belonging to the owner, not a coding task. Recorded so it is not mistaken for engineering work.
- **Fiscal invoice path.** Depends on fiscal identity. Has both a business and an engineering component.

Two are closed on arrival, with the reason recorded:

- **Prototype counter and table workflows** — deferred to Phase 11, where the frontend is built.
- **Define durable sales model and module seams** — resolved. ADR-001 through ADR-046 and the implemented vertical-slice layout answer it. Carrying it forward as open would misrepresent the state of the system.

---

## 8. ROADMAP.md

`ROADMAP.md` becomes the only document updated as work lands. It contains:

1. A one-paragraph statement of where the project is: Phases 0 through 6C complete, migration closed, remaining work designed from `CONTEXT.md`.
2. A status table covering every phase, 0 through 12, with its status, its design specification link where one exists, and its backlog ticket link where one does not.
3. The dependency ordering for phases 07 through 12.
4. Pointers to `CONTEXT.md`, `spec/decisions.md`, `docs/superpowers/specs/`, `docs/backlog/`, and `MIGRATE_PLAN.md`, each with its job from the table in Section 3.1.

The status table corrects a defect in the current `MIGRATE_PLAN.md` tracker, which records Preparation as `PENDING 2/3` although its own Phase 6C checklist is complete and committed as `3c6f61e`.

`ROADMAP.md` does not restate acceptance criteria. Those live in the backlog tickets.

---

## 9. MIGRATE_PLAN.md

The file is kept and its body is not edited. A short banner is prepended:

- The migration is closed, with its date.
- `ROADMAP.md` supersedes it for current status.
- The document remains accurate as a record of Phases 0 through 6C and is retained for the design decisions and ADR references recorded in its checklists.
- Its Phase 7 section is superseded by backlog Phase 11.

Editing the body would destroy the record of what was planned against what was found, which several ADRs reference.

---

## 10. New Architecture Decisions

**ADR-047 — Migration closed; pos-cafe is its own authority.** Records that the Go system has passed its TypeScript source; that `CONTEXT.md` now lives in this repository and is the binding domain authority; that references to `cafe-pos/src` and `cafe-pos/.scratch` inside Phase 0 through 6C specifications are historical provenance carrying no live authority; and that work from Phase 07 onward is designed rather than ported. Consequence: a future session must not treat the TypeScript repository as a specification source.

**ADR-048 — Authorize current capabilities inside domain transactions.** Adopted from `cafe-pos/docs/adr/0002`. The authenticated request context is not an authority token: every domain command reloads the acting identity's current enabled state, roles, and capabilities inside its own transaction, before the idempotency claim and before any replay; Manager-only actions additionally require the current `MANAGER` role and a fresh PIN bound to the exact action. Every Go slice already implements this, and ADR-009 and ADR-038 record specific applications of it, but no existing record states it as the cross-cutting rule. The transport-specific final clause of the TypeScript original is restated for Echo: the HTTP layer authenticates transport context, validates input, and maps errors, and never becomes the sole authorization point.

---

## 11. Verification

This change ships documents, so verification is textual rather than behavioral.

1. **No dangling authority.** Searching the repository for `cafe-pos/CONTEXT.md` returns no matches, and the fifteen citations that carried that path now read `CONTEXT.md` and resolve to the new root file.
2. **Every citation resolves.** Every relative link in `ROADMAP.md`, `docs/backlog/*.md`, and the closure banner points at a file that exists.
3. **Glossary integrity.** `CONTEXT.md` in `pos-cafe` is byte-identical to the `cafe-pos` original.
4. **Ticket completeness.** Every acceptance criterion in TypeScript tickets 12 through 17 appears in exactly one Go backlog ticket, marked `(Phase 11 — UI)` where applicable. None is silently dropped.
5. **Dependency consistency.** Every phase named in a `Blocked by` line exists, and the graph is acyclic.
6. **Status truth.** The `ROADMAP.md` status table matches the committed state of the repository, including Phase 6C as complete.
7. **Build unaffected.** `go build ./...` and `go vet ./...` still pass; no Go file is touched by this change.

---

## 12. Accepted Consequences

Dropping the nine completed effort directories loses the original acceptance criteria for Phases 0 through 6C. This is accepted: those criteria were written against tRPC, Drizzle, and HeroUI, the Go specifications restate the behavior that survived, and `cafe-pos` continues to exist on disk for anyone who needs the original wording.

Renumbering breaks continuity with any external reference to TypeScript ticket numbers. The `Source:` line in each ticket makes the mapping recoverable, and no such external reference is known.

Marking a ticket `ready-for-design` rather than `ready-for-agent` means none of the six can be picked up and implemented directly. That is the accurate state, not a regression: no approved Go design exists for any of them.
