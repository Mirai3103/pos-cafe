# Phase 11: Serve clients over LAN and single-binary packaging

**Status:** ready-for-design
**Blocked by:** none
**Source:** cafe-pos `.scratch/opening-day-pos-v0/issues/16-serve-supported-clients-over-lan.md`,
combined with `MIGRATE_PLAN.md` Phase 7

**What to build:** Package and expose the working Core POS as one authoritative local
service that the cashier station, Preparation Queue display, and staff phones can
reach over the staff LAN without public Internet dependence — and build the frontend
that consumes it.

This is the phase the current effort defers. It is recorded in full so that "skip
the UI for now" does not become "lose the UI requirements."

## Why two tickets became one

The source ticket describes LAN binding, health, the Local Access QR, and a
production smoke test. `MIGRATE_PLAN.md` Phase 7 describes OpenAPI client
generation, static asset embedding, and hardware verification. Both describe the same
deliverable: one authoritative local service the cafe's clients can reach. Splitting
them would put one phase in two trackers.

The source ticket's first criterion — "the production TanStack Start application
builds into a standalone Node-compatible server artifact" — does not survive
migration. Go replaces it with a single static binary, which is the stronger form of
the same requirement and the reason the migration was undertaken.

## Ordering

This phase carries no `Blocked by` edge: its source depends only on delivered work,
and packaging and LAN binding are independent of every remaining phase.

Its generated-client half is order-sensitive rather than blocked. Phases 07 through
10 each add routes, so a TypeScript client generated before them goes stale. Prove
packaging, binding, and the hardware budget early; regenerate the client after the
last API-changing phase.

## Already present in Go

- **`/health` already exists** in `cmd/api/main.go` and reports database reachability
  without exposing secrets.
- **Migrations are already embedded** via `embed.FS` and run automatically on startup
  (Phase 0), which is most of "starts against a migrated PostgreSQL database."
- **CORS is already configuration-driven** through `CORSAllowedOrigins`, so
  restricting cross-origin access is configuration, not new code.
- **Swagger/OpenAPI 2.0 is already generated and served** at `/swagger/index.html`,
  covering every route across all six slices. The client-generation input exists.
- **Port is already configurable**; host is not. The server currently binds `:PORT`,
  meaning every interface. Binding to an intended staff-LAN interface is a real gap.

## Acceptance criteria

- [ ] The application builds into a single self-contained binary with no runtime
      dependency beyond PostgreSQL, and starts against a migrated database.
- [ ] Explicit host and port configuration binds the application to the intended
      staff-LAN interface without broad cross-origin access.
- [ ] A health endpoint reports application readiness and version without exposing
      secrets or protected business data.
- [ ] A Local Access QR presents only the configured stable staff-LAN URL and grants
      no credential, token, role, or authentication bypass.
- [ ] The complete cash takeaway tracer bullet remains usable when public Internet
      access is disconnected but the server and staff LAN are healthy.
- [ ] The browser never queues authoritative offline commands; a lost server or LAN
      connection presents a clear fallback message instead of accepting hidden work.
- [ ] Production configuration does not expose demo identities, sample sales,
      development tooling, or unrestricted public endpoints.
- [ ] Runtime configuration and application-version identity are documented and
      independently recoverable from source code.
- [ ] A production smoke test builds, starts, checks health, rejects anonymous
      protected access, authenticates, and completes a representative protected read
      over the configured network binding.
- [ ] TypeScript types are generated from `/swagger/doc.json` and the frontend calls
      the REST API through them, replacing tRPC. (Phase 11 — UI)
- [ ] The built frontend is embedded in the binary and served by Echo, with unknown
      paths falling through to the client entry point. (Phase 11 — UI)
- [ ] Sign-in, Cashier, and Preparation Queue screens remain usable at the target
      desktop-display and supported-phone sizes. (Phase 11 — UI)
- [ ] Resident memory stays under 30 MB and cold start under 50 ms on the target
      low-spec terminal.

## Design notes carried from the source

The memory and startup budgets are the migration's original justification, restated
here so they are measured rather than assumed. `MIGRATE_PLAN.md` states the boot
target twice and inconsistently — under 20 ms in its header goal, under 50 ms in its
Phase 7 checklist. The looser figure is used above; the specification for this phase
should settle on one and say which artifact it measures, since a binary serving
embedded frontend assets is not the same artifact as a bare API server.

"The browser never queues authoritative offline commands" is a product rule, not a
performance note. `CONTEXT.md` states it in the definition of Supported POS Client:
no client owns an offline transaction queue. Any client-side retry must be a retry of
an idempotent request, never a local write awaiting sync.

## Open questions

- [Prototype counter and table workflows](open-questions.md#prototype-counter-and-table-workflows)
  — the source design ticket deferred to this phase.
- [Receipt and PDF boundary](open-questions.md#receipt-and-pdf-boundary) — whether
  the cashier station must render or print a customer document.
