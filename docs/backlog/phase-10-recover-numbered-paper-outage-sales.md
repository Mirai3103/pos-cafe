# Phase 10: Recover numbered-paper outage Sales

**Status:** ready-for-design
**Blocked by:** 09
**Source:** cafe-pos `.scratch/opening-day-pos-v0/issues/15-recover-numbered-paper-outage-sales.md`

**What to build:** Give staff a controlled bridge from the cafe's paper fallback back
into the Core POS, accounting for every numbered outage sale exactly once without
replaying preparation that already happened.

`CONTEXT.md` defines the four terms this phase implements: Outage Sale Record,
Outage Recovery Entry, POS Service Incident, and the five-minute threshold that
begins one.

## Already present in Go

- **Immutable historical snapshots are the established pattern.** Committed Items
  already carry item name, size, modifiers, and price at commit time, so
  reconstructing a paper sale does not require inventing a snapshot mechanism.
- **Idempotency by caller-supplied key** (ADR-007) gives the one-entry-per-paper-number
  guarantee its natural implementation.
- **Post-Shift correction linkage** arrives with Phase 09, which is why this phase
  depends on it: a recovered sale whose original Shift has closed must land as a
  linked correction rather than reopening the Shift.
- **Submit is the queue-creation boundary** (ADR-033), so suppressing Preparation
  Unit creation for already-fulfilled paper work is a decision at one known place
  rather than a search for every path that could enqueue.

Everything else is new: the incident record, the numbered-entry reconstruction
command, gap and duplicate visibility, and the two-client recovery check.

## Acceptance criteria

- [ ] Any serving staff member can declare a POS Service Incident after the local
      service or staff LAN has failed to recover within five minutes.
- [ ] The incident records observed start, responsible staff, last known live
      transaction, observed symptom, and first numbered Outage Sale Record.
- [ ] A Manager can enter each numbered paper record with actual occurrence time,
      service context, item snapshots, modifiers, totals, Payment method, cash tender
      and change where applicable, fulfillment, staff, and corrections.
- [ ] Exactly one idempotent Outage Recovery Entry can exist for each paper number,
      and missing or duplicate numbers remain visible.
- [ ] Already prepared or fulfilled paper work is never sent to the live Preparation
      Queue during reconstruction.
- [ ] If the original Sales Shift remains open, recovered money and sales participate
      in its reconciliation and block closure until reconstruction is complete.
- [ ] If the original Shift is closed, recovery preserves its snapshot and uses linked
      Post-Shift corrections without reopening it.
- [ ] Both primary clients must pass an explicit recovery check before the incident
      can end; the incident then retains end time and full paper-number range.
- [ ] Integration tests cover open-Shift and closed-Shift recovery, fulfilled work,
      idempotency, paper gaps, role enforcement, audit history, and concurrent
      duplicate entry.
- [ ] The recovery interface clearly separates live sales from historical
      reconstruction and requires confirmation before creating each entry.
      (Phase 11 — UI)

## Design notes carried from the source

An Outage Recovery Entry carries an occurrence time in the past while being created
now. Every other durable record in this system uses a server-authoritative instant
at the moment of writing. The specification must state how both times are held
without letting a backdated entry masquerade as a live one — the source ticket is
explicit that it must not "pretend the entry occurred while the system was
available."

The closure blocker in the sixth criterion interacts with the Phase 07 precedence
list and must be added to it, not implemented separately.

## Open questions

- [Receipt and PDF boundary](open-questions.md#receipt-and-pdf-boundary) — a
  recovered sale may need the same customer document a live sale produces.
