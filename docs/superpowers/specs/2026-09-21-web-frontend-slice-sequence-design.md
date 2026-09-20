# Design Specification: Web Frontend Slice Sequence (`./web`, Phase 11 UI)

- **Author:** Claude Opus 5 & Team
- **Date:** 2026-09-21
- **Status:** Draft, pending review
- **Phase:** Phase 11, UI half — see [`docs/backlog/phase-11-clients-over-lan-and-single-binary.md`](../../backlog/phase-11-clients-over-lan-and-single-binary.md)
- **Predecessor:** [`2026-09-20-web-architecture-design.md`](2026-09-20-web-architecture-design.md) (foundation, approved)
- **Visual authority:** [`design-system/pos-cafe/DESIGN.md`](../../../design-system/pos-cafe/DESIGN.md) and the seven prototype pages beside it
- **Domain authority:** [`CONTEXT.md`](../../../CONTEXT.md), [`spec/decisions.md`](../../../spec/decisions.md)

---

## 1. Purpose

The predecessor spec delivered the frontend foundation: feature directories,
TanStack routing, Crisp Emerald design tokens in `web/src/index.css`, orval
codegen against `docs/swagger.yaml`, and seven placeholder screens. Every one of
those seven screens renders mock data. No screen calls the Go API.

This spec does not design a screen. It fixes **the order in which the seven
placeholders are replaced by real, API-backed screens**, the definition of done
that each replacement must meet, and the shared infrastructure the first slice
must create because later slices depend on it.

The driving constraint is the operator's: ship a few screens end to end at a
time, never the whole frontend in one change, because a large change cannot be
reviewed or controlled.

### Goals

1. Order the nine delivery slices by domain dependency rather than by visual
   appeal or screen size.
2. Give every slice one identical, non-negotiable definition of done.
3. Concentrate the cross-cutting primitives — session, capability guard, Manager
   Approval, request identity, envelope unwrapping — in slice 1, so slices 2
   through 9 consume rather than reinvent them.
4. Isolate the blast radius of client regeneration when Phases 08 through 10 add
   routes.
5. Name the one gap that blocks acceptance testing (no seed data) and close it.

### Non-Goals

- Designing the internals of any individual screen. Each slice opens with its
  own short design against the corresponding prototype page.
- The packaging, LAN-binding, and Local Access QR half of Phase 11.
- Customer receipts of any kind. See section 7.

---

## 2. Delivery order

Domain dependency, not preference, fixes this order:

```
auth session ──> open Sales Shift ──> POS (draft → commit → pay → submit) ──> KDS ──> History
                                           ↑
                                      catalog menu                    Tables (dine-in)
```

- Every business endpoint is protected, so no screen functions without a Staff
  Access Session.
- No money may be taken without an open Sales Shift, so POS cannot run end to
  end before Shift.
- The Preparation Queue has nothing to display until POS can submit.
- History has no rows until a Completed Sale exists.
- Tables and Settings block nothing.

| Slice | Screen | Primary endpoints | Why here |
| :--- | :--- | :--- | :--- |
| **1** | Sign-in, Workspace, Lock/Unlock | `/auth/*` | Blocks everything else. Creates the shared primitives in section 4. |
| **2** | Shift: open, cash movements, close and reconcile | `/shifts/*` | POS cannot take money without an open Shift. Form-heavy and state-light, so it settles the command-and-reason pattern cheaply. |
| **3** | POS-a: sellable menu and Order Draft | `/catalog/menu/sellable`, `/sales/service-sessions/takeaway`, `/draft/items/*` | The largest screen in the product, split into three. This third reads catalog and edits a draft; it touches no money. |
| **4** | POS-b: commit, Check, cash payment | `/draft/commit`, `/checks/{id}/payments/cash` | The money path. Closes the Phase 11 cash takeaway tracer bullet. |
| **5** | POS-c: submit, close session, Completed Sale | `/submit`, `/close`, `/completed-sale` | Produces the data slices 6 and 8 display. |
| **6** | KDS, the Preparation Queue | `/preparation/queue`, `/preparation/units/*` | Has work to show only after slice 5. Named in Phase 11 acceptance. |
| **7** | Tables, dine-in | `/tables/overview`, `/service-sessions/dine-in` | Independent branch; blocks nothing. |
| **8** | History | `/sales/completed-sales/*` | Phase 09 reshapes post-Shift corrections and Audit history. Last, to be built once. |
| **9** | Settings and catalog administration | `/catalog/*`, `/staff/*` | The widest endpoint surface and the least urgent: the cafe still sells if the menu is changed by script. |

Slices 1 through 6 satisfy the Phase 11 UI acceptance criterion that Sign-in,
Cashier, and Preparation Queue remain usable. Slices 7 through 9 are additional.

### Regeneration risk

Phase 11 records that a TypeScript client generated before Phases 07 through 10
goes stale. Slices 1 through 6 sit entirely on closed slices — `auth`, `shift`,
`catalog`, `sales`, `preparation` — and are safe today. Slice 8 is the exposed
one, which is why it is ordered last rather than by value.

---

## 3. Definition of done

Identical for every slice. A slice is not done until all of it holds.

1. The screen calls the real Go API through the generated client. No mock data
   survives in the shipped path.
2. Loading, empty, and error states render from the real error envelope, not
   from invented states.
3. Routes and actions are guarded by the session's `capabilities`.
4. Every mutation carries a `request_id` generated per user intent (section 4.3).
5. The screen follows its prototype page in `design-system/pos-cafe/pages/`,
   with omissions recorded per section 7.
6. Basic tests only: pure logic and store transitions. **No end-to-end tests and
   no integration tests** — the Go side already carries PostgreSQL integration
   coverage, and duplicating it in the browser slows each slice without adding
   signal.
7. **The slice ends at a UAT gate.** Implementation hands over a list of actions
   to perform and results to expect, and stops. The operator confirms the UI and
   the behavior. Completion is never self-certified.
8. One pull request per slice.

---

## 4. Shared infrastructure, created in slice 1

### 4.1 Envelope unwrapping at a seam

Every response is `{success, data, error:{code,message}}` (`internal/response/response.go`).
Orval therefore generates `PostAuthSignIn200 = ResponseAPIResponse & { data?: AuthSignInResponse }`:
`data` is optional on every hook.

Each feature owns an `api/` directory that wraps the generated hook:

```
features/shift/api/use-current-shift.ts  →  useGetShiftsCurrent({ query: { select: unwrap } })
```

`unwrap<T>(res)` throws `ApiError` when `success === false` or `data == null`,
and otherwise returns `data`.

`web/src/api/generated/` is orval output and is overwritten by `bun run codegen`.
The `api/` seam is where regeneration churn stops; components never see it.

**Rejected:** unwrapping inside `customAxiosInstance`. The generated types would
then misdescribe the runtime shape — a silent class of bug worse than the
verbosity it removes.

### 4.2 `ApiError` and the error code table

The axios response interceptor constructs `ApiError {status, code, message}`
from the envelope. `lib/error-messages.ts` maps stable codes to Vietnamese,
falling back to the server message. Three conditions are handled once at the
infrastructure layer rather than in each screen:

| Condition | Handling |
| :--- | :--- |
| `401 UNAUTHORIZED` | Clear session, route to `/auth/login` |
| `403 FORBIDDEN` | Ambiguous, see below |
| `429 TOO_MANY_REQUESTS` | Surface a retry message; sign-in and unlock are rate limited |

`internal/auth/middleware.go` returns the same `403 FORBIDDEN` for a locked
session and for a missing role, differing only in a Vietnamese message. A client
must not branch on message text. On any `403`, re-read `GET /auth/session`,
which is registered outside `RequireAuth` and therefore answers while locked: a
`locked` state raises the lock overlay, anything else is a genuine authority
denial. The extra round trip is paid only on a rare path.

### 4.3 `request_id` is generated per intent, not per call

`crypto.randomUUID()` is generated once when the operator commits to an action
and is reused across every retry and repeated press of that same action. An id
generated inside the calling function turns each retry into a distinct command —
which is exactly the cashier pressing "pay" twice on a slow network and the cafe
losing money. `lib/command.ts` owns the helper, and every mutation goes through
it.

### 4.4 Server state stays on the server

Order Draft, Check, Shift, and Preparation Queue are server-authoritative.
They live in the react-query cache with optimistic updates. Zustand holds only
the Staff Access Session and genuinely client-local UI state such as the open
tab or expanded panel.

This supersedes the predecessor spec's `stores/use-pos-store.ts` ("cart items,
active order, discount") and `stores/use-shift-store.ts` ("active shift info").
Recorded as **ADR-053**.

### 4.5 The three primitives

| Primitive | Location | Built in | Consumed by |
| :--- | :--- | :--- | :--- |
| Session store: token, profile, `capabilities[]`, workspace, `state` | `src/stores/use-session-store.ts` | Slice 1 | Every slice |
| `unwrap` and `ApiError` | `src/lib/unwrap.ts` | Slice 1 | Every slice |
| `requireAuthenticated()` and `requireCapability()` in `beforeLoad` | `src/lib/guards.ts` | Slice 1 | Slices 2 through 9 |
| `newRequestId()` and the command wrapper | `src/lib/command.ts` | **Slice 2** | Slices 2 through 9 |
| `<ManagerApprovalDialog>`: collects a fresh `manager_pin` | `src/components/feedback/manager-approval-dialog.tsx` | **Slice 2** | Slices 2, 4, 5, 6, 9 |

The last two are built in slice 2, not slice 1, because no auth command takes
either one: `SignInRequest`, `UnlockRequest`, and `DeclareWorkspaceRequest` carry
no `request_id` and no `manager_pin`, while `shift.OpenShiftCommand` carries a
`request_id` and shift closure needs Manager Approval. Building them in slice 1
would ship two untestable components with no caller. The rule in section 4.3
still binds from the moment the first command exists.

`_app` currently has no guard at all. Today that is harmless because its
children are placeholders; from slice 1 they call real APIs, so the guard ships
with slice 1.

---

## 5. Slice 1 in detail: the Staff Access Session

Slice 1 is not a login form. It is the session lifecycle the auth API actually
models, across three surfaces.

**Sign-in** (`/auth/login`): `login_code` plus a 4 to 8 digit PIN on a touch
keypad at `--spacing-touch`, ported from `design-system/pos-cafe/pages/auth.html`.
`postAuthSignIn` returns `{token, staff{roles, capabilities}}`.

**Workspace declaration**: immediately after sign-in, one of `cashier`,
`manager`, or `preparation` via `postAuthWorkspace`. This is not decoration —
`CONTEXT.md` defines Staff Workspace as what the session's inactivity lock
policy depends on, because a preparation display tolerates longer idle time than
a counter station. The device remembers the previous choice so the next sign-in
is one tap.

**Lock overlay**: full-screen when the session state is `locked`, accepting only
the signed-in identity's PIN via `postAuthUnlock`. `CONTEXT.md` states that lock
suspends authority without ending the session, so the overlay must not reset
router state or discard the token.

**Application start**: `__root.beforeLoad` calls `getAuthSession`, whose `state`
is `authenticated`, `locked`, or `signed_out`, and hydrates the store from it.
`localStorage` is never the source of truth; the token is only what the request
interceptor attaches.

**Activity**: `postAuthActivity`, throttled to roughly 30 seconds and only on
real interaction. The server owns the lock policy; the client reports activity
and reacts when the server reports `locked`. A manual lock button in the header
calls `postAuthLock`.

**Excluded from slice 1:** first-manager bootstrap (`postAuthBootstrap`, handled
by the dev seed), staff administration (`/staff/*`, slice 9), and the identity
suggestion list (`getAuthIdentities`, added later only if typing login codes
proves tedious).

**Tests:** session store transitions (`signed_out → authenticated → locked →
authenticated`), `unwrap`, and the error code map. No fake server.

**UAT script:** wrong PIN, correct PIN, workspace selection, manual lock,
unlock, and a page refresh mid-session.

---

## 6. Porting the prototype

`design-system/pos-cafe/` is a visual specification, not source to convert.
`index.html` is 4,732 lines and `shared/pos-bus.js` is 1,281 lines of
`BroadcastChannel` plus `localStorage` state over seeded mock data.

The method: read the prototype for layout structure, density, spacing, and
Vietnamese wording, then rebuild with the primitives in `components/ui/`.
**`pos-bus.js` is discarded entirely** — react-query against the real API serves
its cross-tab purpose. Inline prototype JavaScript is not copied.

The POS view must not become one enormous file. The three-way split of slices 3,
4, and 5 exists partly to force `menu-grid`, `draft-panel`, and `payment-sheet`
into separate components. Any view file crossing roughly 300 lines is split at
that moment, not later.

---

## 7. Risks

**No data to test against.** `sql/init/` contains only the test-database script
and the Makefile has no seed target. A clean database has no Manager to sign in
as, so slice 1 cannot be acceptance-tested, and no catalog items, so slice 3
cannot either. Slice 1 therefore ships `scripts/dev-seed.ts`, run by bun against
the API (`POST /auth/bootstrap`, then `/catalog/*`), plus a `make dev-seed`
target. It is explicitly a development script: Phase 11 acceptance forbids
production configuration from exposing demo identities or sample sales.

**The prototype draws what no API serves.** `DESIGN.md` advertises a thermal
receipt simulator and `settings.html` runs to 5,884 lines. But
[`docs/backlog/open-questions.md`](../../backlog/open-questions.md) records that
no Go slice produces a customer receipt, that the receipt boundary is unresolved,
and that it blocks the Phase 11 question of whether the cashier station renders
or prints anything. **Receipt printing is out of scope for all nine slices.**
Each slice opens by reconciling its prototype page against endpoints that exist
and cutting what has none, recording the cut so the requirement is not lost.

**Already sound, and not to be disturbed:** `vite.config.ts` proxies `/api`,
`/swagger`, and `/health` to `localhost:8080`, and `make build` builds the web
bundle and embeds it in the Go binary.

---

## 8. Explicit non-goals

Offline command queuing (Phase 11 forbids the browser queuing authoritative work
behind a lost connection), realtime push for KDS (slice 6 polls with react-query;
WebSocket only if polling is measured to be insufficient), dark mode,
internationalization (Vietnamese is fixed), a bootstrap-manager screen, and the
Local Access QR, which belongs to the packaging half of Phase 11.

---

## 9. Testing strategy

Basic tests only, run with `bun test` (bun is already the package manager and
`web/package.json` currently has no test runner at all). Covered: store
transitions, `unwrap`, the error code map, and VND formatting. A `"test":
"bun test"` script is added in slice 1.

Vitest, happy-dom, and Testing Library are deliberately **not** added. They are
infrastructure for component and integration testing, which this project does
not want in the browser layer. Each slice's real acceptance signal is the UAT
gate in section 3.
