# POS Cafe

[![Go Version](https://img.shields.io/badge/Go-1.27+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Database](https://img.shields.io/badge/Database-PostgreSQL%2017-blue?style=flat&logo=postgresql)](https://www.postgresql.org)
[![Swagger](https://img.shields.io/badge/Swagger-OpenAPI%202.0-green?style=flat&logo=swagger)](https://swagger.io)

A point-of-sale system for a single cafe: a Go backend organised as **vertical
slices** over PostgreSQL, and a React frontend in `web/` that is embedded into the
same binary.

The domain language is defined in [`CONTEXT.md`](CONTEXT.md) and is binding.
Architecture decisions live in [`spec/decisions.md`](spec/decisions.md), and current
status and remaining work in [`ROADMAP.md`](ROADMAP.md).

---

## Key Ideas

- **Idiomatic Go:** explicit wiring in `cmd/api/main.go`, no DI container, no
  mediator, no reflection-based dispatch. Handlers are plain structs with a typed
  `Handle` method.
- **Vertical slices:** each business area (`auth`, `catalog`, `tables`, `shift`,
  `sales`, `preparation`) owns its routes, HTTP handlers, domain rules, DTOs,
  errors, and use cases. Business slices never import one another — they depend
  only on `auth` (middleware, Manager approval), `platform`, and `response` — and
  read another slice's tables through their own sqlc queries (ADR-006).
- **One shared command pipeline:** every authorized mutation and read runs through
  `internal/platform/command`, which reloads the actor's authority inside the
  transaction, checks capabilities, enforces idempotency, and writes the audit
  event (ADR-005, ADR-048). Slices differ only in the `Policy` they declare.
- **Type-safe SQL with `sqlc`:** queries are plain SQL in `sql/queries/`, compiled to
  Go. No ORM.
- **Embedded migrations:** schema migrations are embedded with `embed.FS` and applied
  on startup. Forward-only.
- **Money is whole VND** stored as `BIGINT`.

---

## Tech Stack

| Component | Technology |
| :--- | :--- |
| **Language** | Go 1.27+ |
| **HTTP** | [Echo v4](https://echo.labstack.com/) |
| **Database** | PostgreSQL 17 via [jackc/pgx/v5](https://github.com/jackc/pgx) (`database/sql` compatibility) |
| **Data access** | [sqlc](https://sqlc.dev/) |
| **Logging** | `log/slog` (JSON) |
| **Configuration** | Environment variables, `.env` via [joho/godotenv](https://github.com/joho/godotenv) |
| **API docs** | [swaggo/swag](https://github.com/swaggo/swag), Swagger UI at `/swagger/index.html` |
| **Testing** | [stretchr/testify](https://github.com/stretchr/testify), per-package PostgreSQL clones |
| **Frontend** | React 19, Vite, TanStack Router and Query, Tailwind CSS, [orval](https://orval.dev/)-generated client, [bun](https://bun.sh/) |

---

## Project Structure

```text
pos-cafe/
├── cmd/api/                    # Entrypoint: explicit wiring, graceful shutdown
├── api/openapi/                # Generated Swagger 2.0 (swag): docs.go, swagger.json, swagger.yaml
├── internal/
│   ├── platform/
│   │   ├── command/            # Shared mutation/read pipeline: in-tx authority reload, capability
│   │   │                       #   check, optional Gate (self Manager PIN) / second-party Approval,
│   │   │                       #   idempotency (pluggable IdempotencyStore), audit, advisory lock;
│   │   │                       #   per-slice Policy
│   │   ├── httpx/              # Shared Echo helpers: actor, UUID params, body binding, results/errors
│   │   ├── config/             # Environment configuration
│   │   └── database/           # pgx pool, embedded migrations (migrations/), sqlc code (sqlc/)
│   ├── response/               # API envelope + table-driven ErrorMapper
│   ├── auth/                   # Staff access sessions, PIN sign-in, rate limiting, staff administration
│   ├── catalog/                # Menu, categories, sizes, modifier groups, availability, images
│   ├── tables/                 # Tables and table overview
│   ├── shift/                  # Sales Shift, Cash Movements, closure and reconciliation
│   ├── sales/                  # Service Sessions, Order Drafts, Checks, Payments, corrections
│   ├── preparation/            # Preparation Queue, alerts, waste, remake, cancellation
│   └── testdb/                 # Ephemeral per-package Postgres clones for integration tests
├── sql/
│   ├── queries/                # sqlc query files (platform.sql = shared authority/idempotency queries)
│   └── init/                   # Creates cafe_pos_test on first Postgres boot
├── web/                        # React frontend (see below); embed.go serves the built SPA from the binary
├── docs/                       # Specs, plans, backlog, domain rationale, history/, reports/
├── spec/                       # Architecture decision records (decisions.md)
├── design-system/              # Static HTML prototype + its checks (pos-cafe/tests/)
├── scripts/                    # Development seed scripts (run with bun)
├── resources/seeds/            # Demo menu images used by the dev seed
├── CONTEXT.md                  # Domain language (binding)
├── ROADMAP.md                  # Current status and remaining phases
├── Makefile
└── sqlc.yaml
```

### Anatomy of a slice

Each business slice under `internal/` follows the same shape. `internal/tables` is
the smallest and a good reference:

| File | Role |
| :--- | :--- |
| `routes.go` | `NewSlices(db, queries)` builds one `Runner` and every handler; `RegisterRoutes` mounts them on `/api/v1` behind `RequireAuth` / `RequireCapability`. |
| `http*.go` | Echo handlers with swag annotations. They read the actor, bind the body, validate the `request_id`, call the use case, and write the result through `httpx`. |
| `executor.go` | The slice's `command.Policy` and thin wrappers over `command.ExecuteMutation` / `command.ExecuteRead`. |
| `domain.go` | Capabilities, operation names, audit event types, and pure business rules. |
| `dto.go` | Request commands and response types. |
| `errors.go` | Domain sentinels, `MapDBError`, and the slice's `response.ErrorMapper`. |
| One file per use case (or small group) | e.g. `commands.go` (create, rename, set availability), `overview.go` (read). |
| `*_test.go` / `*_integration_test.go` | Unit tests and database-backed tests (`//go:build integration`). |

### The command pipeline

`command.ExecuteMutation` runs a mutation in one read-write transaction, in this
order:

1. reload the actor's session and identity (authority) inside the transaction;
2. verify the spec's required capabilities;
3. run the optional `Gate` (e.g. a self Manager-PIN re-authentication);
4. verify the optional second-party Manager `Approval`;
5. fingerprint the business input (never a credential);
6. take an advisory lock on (actor, request id);
7. replay a stored result for an exact duplicate, or reject a reused request id
   with `ErrRequestConflict`;
8. claim the request in the policy's `IdempotencyStore`;
9. run the slice's body;
10. write the business audit event, when the body returns one;
11. store the replayable result and commit.

Authority and gates are re-checked before a replay, so a revoked session, a lost
role, or a rotated PIN cannot replay an earlier success. Denials are audited under
the slice's own event type.

`command.ExecuteRead` runs a read in a read-only `REPEATABLE READ` transaction with
the same authority reload and capability check, so the check and every query in the
body observe one snapshot.

What differs between slices is declared once in a `command.Policy`: the denial
event type and log scope, whether the actor is attributed on a denial, what a failed
denial audit returns, whether read denials are audited, which slice errors count as
security denials, the sentinel for a rejected approval, and the idempotency store
(`command.IdempotencyKeys`, the shared `idempotency_keys` table, by default). The
Tables policy:

```go
var policy = command.Policy{
	DenialEventType:      EventAuthorizationDenied, // "tables.authorization_denied"
	LogScope:             "tables",
	Attribution:          command.AttributeActorIfConfirmed,
	OnDenialAuditFailure: command.ReturnAuditError,
}

func NewRunner(db *sql.DB, queries *sqlc.Queries) *Runner {
	return command.NewRunner(db, queries, policy)
}
```

---

## Getting Started

### Prerequisites

- **Go** 1.27 or higher (see the `go` directive in `go.mod`).
- **Docker + Docker Compose** for the local PostgreSQL instance.
- **bun** for the frontend (`make build`, `make dev-seed`, and everything in `web/`).
- **sqlc** (only when editing SQL queries):
  ```bash
  go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
  ```
- **swag** (only when changing Swagger annotations):
  ```bash
  go install github.com/swaggo/swag/cmd/swag@latest
  ```

`golangci-lint` and `govulncheck` are installed on demand by `make lint` and
`make vuln`.

### Run the backend

```bash
# 1. Start PostgreSQL (creates both cafe_pos and cafe_pos_test on first boot)
make docker-up
make db-wait

# 2. Run the API server
make run
# or: go run ./cmd/api
```

The server:
1. Reads `.env` if present (falling back to the process environment).
2. Connects to PostgreSQL via `DATABASE_URL`.
3. Applies the embedded SQL migrations.
4. Serves the API under `/api/v1`, Swagger UI at `/swagger/index.html`, catalog
   images at `/media`, a health probe at `/health`, and the embedded web build at
   `/` — all on `http://localhost:8080`.

On an empty database, create the first Manager with `POST /api/v1/auth/bootstrap`.
For a full demo data set (staff with PIN `1234`, tables, and the menu with images),
run `make dev-seed` against the running server. It **truncates** the development
database first, so never point it at real data.

### Run the frontend

```bash
cd web
bun install
bun run dev        # Vite on http://localhost:5173, proxies /api, /swagger, /media, /health to :8080
```

`make build` builds the frontend and then compiles one binary with the web build
embedded (`build/app.exe`).

---

## Configuration

Every setting is read from the environment, with `.env` loaded first if present
(see `.env.example`). All variables are optional.

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | HTTP listen port. Validated to be 1–65535. |
| `DATABASE_URL` | `postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos?sslmode=disable` | PostgreSQL DSN. Must not be empty. |
| `MEDIA_DIR` | `./data/media` | Where content-addressed catalog images are stored (ADR-057). Back it up with the database. |
| `APP_ENV` | `development` | Free-form environment label used in logs. |
| `CORS_ALLOWED_ORIGINS` | `*` | Comma-separated allowed origins. |
| `HTTP_READ_TIMEOUT` | `15s` | `http.Server.ReadTimeout` (Go duration string). |
| `HTTP_WRITE_TIMEOUT` | `15s` | `http.Server.WriteTimeout`. |
| `HTTP_IDLE_TIMEOUT` | `60s` | `http.Server.IdleTimeout`. |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | `http.Server.ReadHeaderTimeout` (Slowloris protection). |

Loading fails fast on an invalid `PORT`, an empty `DATABASE_URL`, or a non-positive
timeout — a zero timeout means *no* timeout in `net/http`, which would silently drop
the protection these settings exist to provide.

---

## API

All endpoints live under `/api/v1` and are documented in Swagger UI at
**[http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html)**.
The generated spec is committed in `api/openapi/`; regenerate it after changing
handler annotations:

```bash
make swagger
```

The frontend's typed client is generated from `api/openapi/swagger.yaml` with
`cd web && bun run codegen`.

Conventions every slice follows:

- **Authentication:** `POST /auth/sign-in` with a login code and PIN returns a
  token; send it as `Authorization: Bearer <token>`. Routes additionally require a
  capability (or, for staff administration, the Manager role) derived from the
  staff member's roles.
- **Idempotency:** every mutation body carries a client-generated `request_id`
  (UUID). Resending the same request returns the stored result; reusing the id for
  a different change is a `409 REQUEST_CONFLICT`.
- **Envelope:** responses are `{"success": true, "data": ...}` or
  `{"success": false, "error": {"code": "...", "message": "..."}}`, with the
  code and status chosen by the slice's `ErrorMapper`.

Example — create a Table:

```bash
curl -X POST http://localhost:8080/api/v1/tables \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"request_id": "0b8e5f9e-6d3c-4d2a-9a57-3f7f0c1d2e4b", "name": "Bàn 1"}'
```

```json
{
  "success": true,
  "data": { "id": "…", "name": "Bàn 1", "available": true }
}
```

A second table with the same (normalized) name answers `409` with code
`TABLE_NAME_CONFLICT`; a caller without `tables.administer` gets `403 FORBIDDEN`.

---

## Adding a New Use Case

Walkthrough using the Tables slice: "rename a Table" (`PATCH /tables/:table_id/name`).

### Step 1: Schema and queries

If the use case needs schema changes, add the next numbered migration under
`internal/platform/database/migrations/` (e.g. `000017_….sql`). Add the queries to
the slice's file in `sql/queries/`:

```sql
-- name: GetTableForUpdate :one
SELECT id, name, normalized_name, available, created_at, updated_at
FROM tables
WHERE id = $1
FOR UPDATE;

-- name: RenameTable :one
UPDATE tables
SET name = $2, normalized_name = $3, updated_at = now()
WHERE id = $1
RETURNING id, name, normalized_name, available, created_at, updated_at;
```

Then run `make sqlc`.

### Step 2: Domain names and DTO

In `domain.go`, name the operation and the audit event; reuse or add a capability:

```go
OpRenameTable     = "tables.rename_table"
EventTableRenamed = "TABLE_RENAMED"
```

In `dto.go`, add the command. It always carries a `request_id`; route parameters are
`json:"-"` and filled by the HTTP handler:

```go
type RenameTableCommand struct {
	RequestID uuid.UUID `json:"request_id"`
	TableID   uuid.UUID `json:"-"`
	Name      string    `json:"name"`
}
```

### Step 3: The use case

Build a `MutationSpec` and run the body through the slice's executor. The
fingerprint is the normalized business input. Business preconditions go inside the
body, which runs after the idempotency claim, so a replay of an earlier success
still returns its stored result:

```go
func (h *RenameTableHandler) Handle(ctx context.Context, actor Actor, cmd RenameTableCommand) (int, TableResponse, error) {
	display, key := NormalizeTableName(cmd.Name)
	spec := MutationSpec{
		RequestID:   cmd.RequestID,
		Operation:   OpRenameTable,
		Fingerprint: renameTableFingerprint{TableID: cmd.TableID, Name: display},
		Required:    []string{CapTablesAdminister},
	}

	return ExecuteMutation(ctx, h.runner, actor, spec,
		func(q *sqlc.Queries) (int, TableResponse, AuditRecord, error) {
			if err := ValidateTableName(display); err != nil {
				return 0, TableResponse{}, AuditRecord{}, fmt.Errorf("%w: %s", response.ErrInvalid, err.Error())
			}
			before, err := q.GetTableForUpdate(ctx, cmd.TableID)
			if err != nil {
				return 0, TableResponse{}, AuditRecord{}, MapDBError(err)
			}
			after, err := q.RenameTable(ctx, sqlc.RenameTableParams{
				ID: cmd.TableID, Name: display, NormalizedName: key,
			})
			if err != nil {
				return 0, TableResponse{}, AuditRecord{}, MapDBError(err)
			}
			return 200, toTableResponse(after), AuditRecord{
				EventType: EventTableRenamed,
				Details:   tableRenamedAuditDetails{TableID: after.ID, BeforeName: before.Name, AfterName: after.Name},
			}, nil
		})
}
```

Return a zero `AuditRecord` for a same-state no-op. Slices that need the approver,
the raw `*sql.Tx`, or a `Gate` / `Approval` call `command.ExecuteMutation` with a
`command.MutationContext` body directly (see `internal/sales` and
`internal/shift`). A read uses `ExecuteRead` with the required capability instead,
as `overview.go` does.

### Step 4: Errors

New failure modes get a sentinel in `errors.go`, a case in `MapDBError` if they
come from PostgreSQL, and a row in the slice's `response.ErrorMapper` with their
status and code. Messages that could carry database detail use the sentinel's fixed
text.

### Step 5: HTTP handler and route

Add the handler (with swag annotations) in `http.go`, using the `httpx` helpers:

```go
func (s *Slices) handleRenameTable(c echo.Context) error {
	actor, err := httpx.Actor(c)
	if err != nil {
		return sendError(c, err)
	}
	tableID, err := httpx.UUIDParam(c, "table_id")
	if err != nil {
		return sendError(c, err)
	}
	cmd, err := httpx.BindBody[RenameTableCommand](c)
	if err != nil {
		return sendError(c, err)
	}
	if err := httpx.RequireRequestID(cmd.RequestID); err != nil {
		return sendError(c, err)
	}
	cmd.TableID = tableID

	status, res, err := s.RenameTable.Handle(c.Request().Context(), actor, cmd)
	if err != nil {
		return sendError(c, err)
	}
	return httpx.SendResult(c, status, res)
}
```

Wire the handler in `NewSlices` and mount the route in `RegisterRoutes`:

```go
v1.PATCH("/tables/:table_id/name", s.handleRenameTable,
	authn.RequireAuth(), authn.RequireCapability(CapTablesAdminister))
```

A brand-new slice additionally gets its own `Policy` in `executor.go` and one
`NewSlices(...)` / `RegisterRoutes(...)` pair in `cmd/api/main.go`.

### Step 6: Tests and docs

Add unit tests for pure rules and an integration test against the package's
PostgreSQL clone (see [Testing](#testing)), run `make swagger`, and regenerate the
frontend client if it consumes the endpoint. A design decision that deviates from
`CONTEXT.md` needs an ADR in `spec/decisions.md`.

---

## Testing

Tests are split by the `integration` build tag.

### Unit tests

```bash
make test
# or: go test -race ./...
```

These cover pure domain rules, DTO and error mapping, route registration, and other
logic that needs no database.

### Integration tests (PostgreSQL required)

```bash
make test-integration-fast # local feedback, no race detector
make test-integration      # full race-enabled integration gate
make test-db-clean         # remove inactive clones left by interrupted runs
```

> [!NOTE]
> Each package's `TestMain` calls `testdb.Run`, which provisions an ephemeral clone
> of `cafe_pos_test_template` — a schema-migrated copy of the base test database —
> and runs the whole package against that clone, so package test binaries may
> execute concurrently without wiping each other's fixtures. Direct package
> commands remain supported, e.g.:
>
> ```bash
> TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -v -race -tags=integration ./internal/catalog/
> ```

The `cafe_pos_test` database is created automatically by `sql/init/` the first
time the Postgres volume is initialised. If you have a volume that predates that
script, the hook will not re-run — create the database once with:

```bash
docker compose exec postgres createdb -U cafe_pos cafe_pos_test
# or start from scratch: docker compose down -v && make docker-up
```

> [!IMPORTANT]
> **Test safety guard:** the integration harness requires `TEST_DATABASE_URL` and
> enforces that the base database name ends in `_test` (e.g. `cafe_pos_test`) — the
> template database and its clones are derived from that name. It never falls back
> to the development database.

### Frontend tests

```bash
cd web && bun test
```

### Design-system prototype checks

The static HTML prototype in `design-system/pos-cafe/` has its own Node checks,
unrelated to the application:

```bash
node design-system/pos-cafe/tests/e2e-suite-test.js   # likewise for the other files in that folder
```

### Lint and vulnerability scan

```bash
make lint    # golangci-lint, config in .golangci.yml (schema v2)
make vuln    # govulncheck against the module graph
make check   # fmt + vet + lint + unit tests
```

`make check` is the fast local subset of CI; CI additionally runs the integration
tests and `govulncheck`.

---

## Makefile Commands

| Command | Description |
| :--- | :--- |
| `make docker-up` | Start PostgreSQL (`pos-cafe-db`); creates `cafe_pos` and `cafe_pos_test` on first boot |
| `make docker-down` | Stop PostgreSQL (keeps the data volume) |
| `make docker-logs` | Tail PostgreSQL logs |
| `make db-wait` | Block until PostgreSQL accepts connections |
| `make run` | Run the API server |
| `make build-web` | Build the React frontend with bun |
| `make build-app` | Build the Go binary, embedding the web build, into `build/app.exe` |
| `make build` | `build-web` then `build-app` |
| `make test` | Unit tests with the race detector |
| `make test-integration` | Integration tests with the race detector (needs `docker-up`) |
| `make test-integration-fast` | Integration tests without the race detector |
| `make test-db-clean` | Remove inactive ephemeral integration-test clones |
| `make test-all` | Unit + integration tests |
| `make coverage` | Coverage across unit + integration tests |
| `make fmt` | Format all Go code with `gofmt` |
| `make vet` | Run `go vet` |
| `make lint` | Run `golangci-lint` (installed on demand) |
| `make vuln` | Scan dependencies with `govulncheck` |
| `make check` | fmt + vet + lint + unit tests, before you push |
| `make sqlc` | Regenerate type-safe SQL bindings |
| `make swagger` | Regenerate the Swagger/OpenAPI spec in `api/openapi/` |
| `make tidy` | Run `go mod tidy` |
| `make clean` | Remove build artifacts (`bin/`, `build/`) |
| `make dev-seed` | Reset the dev database and seed demo data via the running API (dev only) |
| `make help` | List all targets |

---

## Documentation Map

| Where | What |
| :--- | :--- |
| [`CONTEXT.md`](CONTEXT.md) | Domain language. Binding. |
| [`ROADMAP.md`](ROADMAP.md) | Current status and remaining phases. |
| [`spec/decisions.md`](spec/decisions.md) | Architecture decision records. Append-only. |
| [`docs/superpowers/specs/`](docs/superpowers/specs/) | Approved per-phase designs. |
| [`docs/superpowers/plans/`](docs/superpowers/plans/) | Implementation plans. |
| [`docs/backlog/`](docs/backlog/) | Work not yet designed, and open questions. |
| [`docs/domain-rationale/`](docs/domain-rationale/) | Why `CONTEXT.md` says what it says. |
| [`docs/history/`](docs/history/) | Frozen historical records (the TypeScript-to-Go migration plan). |
| [`docs/reports/`](docs/reports/) | Test reports (e.g. the manual smoke test). |
| [`docs/agent-prompts/`](docs/agent-prompts/) | Prompts used for agent sessions. |
