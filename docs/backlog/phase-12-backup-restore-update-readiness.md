# Phase 12: Back up, restore, update, and verify readiness

**Status:** ready-for-design
**Blocked by:** 10, 11
**Source:** cafe-pos `.scratch/opening-day-pos-v0/issues/17-backup-restore-update-and-readiness.md`

**What to build:** Make the completed Core POS operable as a cafe appliance: start
and supervise it automatically, protect its data, prove restoration, control updates
and rollback, and give opening staff one Daily POS Readiness Check.

`CONTEXT.md` defines the roles and artifacts this phase implements: POS Operations
Owner, POS Technical Custodian, Recovery Custody Pack, and Daily POS Readiness Check.

## Scope note

This is the only phase that is mostly operations rather than application code. Much
of it is host configuration, scheduled jobs, documented procedure, and drills whose
evidence a human inspects. The specification should say plainly which criteria are
code, which are configuration, and which are written procedure, so the phase is not
mistaken for a coding sprint.

The source ticket assumes a Node deployment. Go changes the shape of several
criteria: supervision covers one binary rather than a Node process tree, and
"application-version identity" becomes a value stamped into the binary at build time
rather than a package manifest read at runtime.

## Already present in Go

- **Embedded auto-migrations** run on startup (Phase 0), which is the mechanism the
  compatibility check below must gate rather than replace.
- **`/health`** exists and is the natural place for the readiness and version report.
- **Single-binary packaging** arrives with Phase 11, which is why this phase depends
  on it.

Everything else is new, and most of it lives outside the Go module.

## Acceptance criteria

- [ ] The local service and PostgreSQL start with the host, restart after ordinary
      process failure, and either become usable within five minutes or present an
      intelligible alert to the POS Operations Owner.
- [ ] Startup checks application-version and schema compatibility and refuses
      business writes against an incompatible or partially migrated database.
- [ ] During operation, recoverable backups limit ordinary server or disk data loss
      to fifteen minutes and use a physically separate local destination.
- [ ] Encrypted off-site backup runs at least daily, queues transfer failure without
      stopping sales, and follows the decided seven local, thirty-five daily, and
      twelve month-end retention targets.
- [ ] Daily integrity checks produce evidence, and a full isolated restore proves
      authentication, business history, Shift reconciliation, and a safe test
      transaction from restored data.
- [ ] Recovery covers authoritative business data, required runtime configuration,
      and application-version identity without placing readable secrets in ordinary
      backup contents.
- [ ] A production update requires approval, a verified pre-update backup, compatible
      migrations, and a recoverable preceding application version.
- [ ] Failed essential post-update checks trigger same-window rollback or restore
      without discarding sales created before maintenance.
- [ ] A Daily POS Readiness Check verifies the local service and LAN, both primary
      clients, individual authentication, system time, latest backup, numbered outage
      forms, Manual QR verification, and one designated on-site Manager for the
      planned Sales Shift before sales.
- [ ] Operations documentation assigns POS Operations Owner and POS Technical
      Custodian responsibilities and describes the owner-controlled Recovery Custody
      Pack and substitute custodian.
- [ ] Automated and human-verifiable tests demonstrate startup supervision, backup,
      restore, migration, rollback, health, and Daily POS Readiness behavior using
      representative Completed Sale and closed-Shift data.

## Design notes carried from the source

"Refuses business writes against an incompatible or partially migrated database"
conflicts with the current startup behavior, which migrates automatically and then
serves. The specification must decide whether auto-migration survives into production
or becomes an explicit, approved step — this is a real change to Phase 0 behavior and
deserves its own ADR.

The restore drill is the criterion most likely to be skipped and the one that makes
the rest meaningful. It should produce a dated artifact, not a passing test.

## Open questions

- [Opening-day readiness](open-questions.md#opening-day-readiness) — the Daily POS
  Readiness Check cannot be defined until this is settled. This is the phase's real
  blocker, and it is a question for the owner, not the codebase.
