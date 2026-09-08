# POS Cafe Backend - Idiomatic Go Vertical Slice Starter

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Architecture](https://img.shields.io/badge/Architecture-Vertical%20Slice%20%2B%20CQRS-orange?style=flat)](https://jimmybogard.com/vertical-slice-architecture/)
[![Database](https://img.shields.io/badge/Database-PostgreSQL%20(pgx%2Fv5)-blue?style=flat&logo=postgresql)](https://github.com/jackc/pgx)
[![Tests](https://img.shields.io/badge/Tests-Passing%20(with%20--race)-brightgreen?style=flat)](https://github.com/stretchr/testify)
[![Swagger](https://img.shields.io/badge/Swagger-OpenAPI%202.0-green?style=flat&logo=swagger)](https://swagger.io)

A production-ready, highly maintainable, and **Idiomatic Golang** backend boilerplate built on **Vertical Slice Architecture** and **CQRS principles**. Designed specifically for Point of Sale (POS) and ordering systems, starting with a clean Cafe POS domain.

---

## 🌟 Key Highlights & Philosophy

- **Idiomatic Go First:** No heavy enterprise C#/Java porting baggage. No reflection-based DI containers (`dig`/`fx`), no opaque mediator layers (`go-mediatr`), no `//go:linkname` runtime hacks.
- **Vertical Slice Architecture (VSA):** Code is sliced vertically by business operation (Command/Query). Each slice encapsulates its request contract, validation, domain logic, database interaction, and HTTP transport.
- **Production-Ready PostgreSQL (`pgx/v5`):** Powered by `jackc/pgx/v5` via standard library compatibility. Robust connection pooling, high throughput, and full support for PostgreSQL types and transactions.
- **Type-Safe SQL with `sqlc`:** Write clean, standard SQL. `sqlc` compiles queries into type-safe Go structs and interfaces with zero runtime reflection.
- **Event-Driven with Watermill:** Built-in in-memory event bus powered by `ThreeDotsLabs/watermill`. Decouple background side-effects (kitchen display, receipt printing, loyalty points) without external message broker setup.
- **Explicit Transactions:** `database.WithTx` runs several sqlc queries inside one transaction with commit/rollback (and panic unwinding) handled for you — the primitive every multi-write slice needs.
- **Zero-Setup Auto-Migrations:** Schema migrations are embedded directly into the binary via Go's standard `embed.FS` and applied on startup.
- **Interactive Swagger UI:** Built-in OpenAPI documentation generated with `swaggo/swag`, available out of the box at `/swagger/index.html`.

---

## 🛠️ Tech Stack

| Component | Technology | Description |
| :--- | :--- | :--- |
| **Language** | Go 1.26+ | Modern Go features |
| **HTTP Framework** | [Echo v4](https://echo.labstack.com/) | High-performance, minimalist HTTP router |
| **Database Driver** | [jackc/pgx/v5](https://github.com/jackc/pgx) | High-performance PostgreSQL driver and toolkit |
| **Data Access** | [sqlc](https://sqlc.dev/) | Compile SQL to type-safe Go code |
| **Event Bus** | [ThreeDotsLabs/watermill](https://github.com/ThreeDotsLabs/watermill) | Industry standard Pub/Sub event bus |
| **Validation** | [go-playground/validator v10](https://github.com/go-playground/validator) | Struct and field validation |
| **Logging** | `log/slog` | Go standard library structured logging |
| **Configuration** | [joho/godotenv](https://github.com/joho/godotenv) | Environment variable and `.env` loader |
| **API Documentation** | [swaggo/swag](https://github.com/swaggo/swag) | Automated Swagger UI / OpenAPI docs |
| **Testing** | [stretchr/testify](https://github.com/stretchr/testify) | Test assertions and test suites |

---

## 📂 Project Structure

```text
pos-cafe/
├── cmd/
│   └── api/
│       └── main.go                 # Application entrypoint: explicit wiring & graceful shutdown
├── config/
│   └── config.go                   # Environment configuration loader with .env support
├── docs/                           # Auto-generated Swagger 2.0 / OpenAPI documentation
│   ├── docs.go
│   ├── swagger.json
│   └── swagger.yaml
├── internal/
│   ├── category/                   # === VERTICAL SLICE: CATEGORY (CQRS) ===
│   │   ├── create_category.go      # Command & Handler: Create category + Publish domain event
│   │   ├── get_category.go         # Query & Handler: Fetch category by ID
│   │   ├── list_categories.go      # Query & Handler: List all / active categories
│   │   ├── update_category.go      # Command & Handler: Update category details
│   │   ├── delete_category.go      # Command & Handler: Delete category
│   │   ├── dto.go                  # Shared category response struct & mapping helpers
│   │   ├── routes.go               # Route registry for Category slices
│   │   └── category_test.go        # End-to-end integration & unit tests
│   ├── database/
│   │   ├── db.go                   # PostgreSQL connection pool setup (pgx/v5)
│   │   ├── tx.go                   # WithTx / WithTxOptions transaction helpers
│   │   ├── migrations/             # Embedded SQL migration files (embed.FS)
│   │   │   └── 000001_init_schema.sql
│   │   └── sqlc/                   # sqlc generated type-safe models & queries
│   │       ├── categories.sql.go
│   │       ├── db.go
│   │       ├── models.go
│   │       └── querier.go          # sqlc.Querier interface for seamless mocking
│   ├── eventbus/                   # === EVENT BUS (Watermill In-Memory) ===
│   │   ├── eventbus.go             # Generic Publish/Subscribe wrapper
│   │   └── eventbus_test.go        # Event bus concurrency test
│   ├── httpvalidator/              # Echo validator adapter over go-playground/validator
│   │   └── httpvalidator.go
│   └── response/                   # Standardized JSON response envelope & HTTP error mapper
│       └── response.go
├── sql/
│   └── queries/                    # SQL source files for sqlc compilation
│       └── categories.sql
├── .env.example                    # Sample environment file
├── .env                            # Local environment configuration
├── .gitignore                      # Git ignore rules
├── Makefile                        # Build, run, test, and code-generation shortcuts
├── sqlc.yaml                       # sqlc code generator configuration
├── go.mod
└── go.sum
```

---

## ⚖️ Why Idiomatic Go?

| Dimension | This Starter Template | Typical C# / Java Port in Go |
| :--- | :--- | :--- |
| **Data Flow** | Direct, typed function calls (`h.Handle(ctx, cmd)`) | Opaque reflection via `mediatr.Send(ctx, cmd)` |
| **Dependency Injection** | Explicit manual wiring in `main.go` | Reflection runtime container (`dig`/`fx`) |
| **Database Access** | Pure SQL compiled to type-safe Go (`sqlc`) | Bulky ORM or custom reflection runtime scanners |
| **Runtime Safety** | 100% standard memory safety, 0 reflection hacks | `//go:linkname` and `unsafe.Pointer` type scanning |
| **Package Structure** | Package by domain feature (`package category`) | Over-fragmented folders (`commands`, `dtos`, `contracts`) |
| **Error Handling** | Standard Go 1.13+ `errors.Is` & `%w` | Deep custom exception hierarchies with stack wrappers |

---

## 🚀 Getting Started

### Prerequisites
- **Go**: Version 1.26 or higher (see the `go` directive in `go.mod`).
- **Docker + Docker Compose**: for the local PostgreSQL instance.
- **sqlc** (optional, only needed when editing SQL queries):
  ```bash
  go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
  ```
- **swag** (optional, only needed when updating Swagger comments):
  ```bash
  go install github.com/swaggo/swag/cmd/swag@latest
  ```

`golangci-lint` and `govulncheck` are installed on demand by `make lint` and
`make vuln`, so there is nothing to set up by hand.

### Quick Run

```bash
# 1. Clone or navigate to the project directory
cd pos-cafe

# 2. Start PostgreSQL (creates both cafe_pos and cafe_pos_test on first boot)
make docker-up
make db-wait

# 3. Run the application
make run
# or: go run ./cmd/api
```

Run `make help` to see every available target.

The application will:
1. Automatically read `.env` (fallback to system environment variables).
2. Connect to PostgreSQL via `DATABASE_URL` (configured in `.env`).
3. Automatically execute embedded SQL migrations.
4. Start the HTTP server at `http://localhost:8080`.

---

## ⚙️ Configuration

Every setting is read from the environment, with `.env` loaded first if present
(see `.env.example`). All variables are optional — the defaults below are what
the template runs with out of the box.

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | HTTP listen port. Validated to be 1–65535. |
| `DATABASE_URL` | `postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos?sslmode=disable` | PostgreSQL DSN. Must not be empty. |
| `APP_ENV` | `development` | Free-form environment label used in logs. |
| `CORS_ALLOWED_ORIGINS` | `*` | Comma-separated allowed origins. |
| `HTTP_READ_TIMEOUT` | `15s` | `http.Server.ReadTimeout` (Go duration string). |
| `HTTP_WRITE_TIMEOUT` | `15s` | `http.Server.WriteTimeout`. |
| `HTTP_IDLE_TIMEOUT` | `60s` | `http.Server.IdleTimeout`. |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | `http.Server.ReadHeaderTimeout` (Slowloris protection). |

`config.Load` fails fast on an invalid `PORT`, an empty `DATABASE_URL`, or a
non-positive timeout — a zero timeout means *no* timeout in `net/http`, which
would silently drop the protection these settings exist to provide.

---

## 🧪 Testing Strategy & Safety

We separate fast in-memory **Unit Tests** from database-backed **Integration Tests** using Go build tags, with strict safety controls protecting development data:

### 1. Pure Unit Tests (Fast & Independent)
Runs completely in-memory using consumer-defined mock interfaces, executing all tests in parallel in **< 0.01s**:
```bash
make test
# or: go test -v -race ./...
```

### 2. Integration Tests (PostgreSQL Required)
Tests run against a dedicated test database (isolated with `//go:build integration` tags):
```bash
make test-integration
# or: TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -v -race -p 1 -tags=integration ./...
```

> [!NOTE]
> `-p 1` is mandatory, not a tuning knob. Every integration package shares the
> single test database and `TRUNCATE`s the same tables in its setup, so letting
> Go run packages in parallel makes them wipe each other's fixtures mid-test.
> Keep the flag when you add integration tests to a new slice.

The `cafe_pos_test` database is created automatically by `sql/init/` the first
time the Postgres volume is initialised. If you have a volume that predates that
script, the hook will not re-run — create the database once with:

```bash
docker compose exec postgres createdb -U cafe_pos cafe_pos_test
# or start from scratch: docker compose down -v && make docker-up
```

> [!IMPORTANT]
> **Zero-Risk Test Safety Guard:** The integration test runner strictly requires `TEST_DATABASE_URL` and enforces that the target database name ends with `_test` (e.g. `cafe_pos_test`). It **never** falls back to your development database and will instantly abort if a non-test database is provided.

### 3. Lint & Vulnerability Scan

```bash
make lint    # golangci-lint, config in .golangci.yml (schema v2)
make vuln    # govulncheck against the module graph
make check   # fmt + vet + lint + unit tests, i.e. what CI enforces
```

---

## 📖 Swagger Documentation

Access the interactive Swagger UI directly in your browser:
👉 **[http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html)**

To regenerate Swagger documentation after adding or modifying API endpoints:
```bash
make swagger
```

---

## 📡 API Reference (`Category` Slice)

### Base URL: `/api/v1`

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/health` | Health check endpoint |
| `POST` | `/api/v1/categories` | Create a new category (triggers domain event) |
| `GET` | `/api/v1/categories` | List categories (supports `?active_only=true`) |
| `GET` | `/api/v1/categories/:id` | Get category details by ID |
| `PUT` | `/api/v1/categories/:id` | Update category by ID |
| `DELETE` | `/api/v1/categories/:id` | Delete category by ID |

### Example cURL Requests

#### 1. Create a Category
```bash
curl -X POST http://localhost:8080/api/v1/categories \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Espresso Bar",
    "description": "Single-origin espresso, Latte, Flat White",
    "display_order": 1,
    "is_active": true
  }'
```

**Response (`201 Created`):**
```json
{
  "success": true,
  "data": {
    "id": 1,
    "name": "Espresso Bar",
    "description": "Single-origin espresso, Latte, Flat White",
    "display_order": 1,
    "is_active": true,
    "created_at": "2026-09-08T20:00:00Z",
    "updated_at": "2026-09-08T20:00:00Z"
  }
}
```

#### 2. Conflict Handling (Duplicate Name)
```bash
curl -X POST http://localhost:8080/api/v1/categories \
  -H "Content-Type: application/json" \
  -d '{"name": "Espresso Bar"}'
```

**Response (`409 Conflict`):**
```json
{
  "success": false,
  "error": {
    "code": "CONFLICT",
    "message": "resource already exists: category with name 'Espresso Bar'"
  }
}
```

#### 3. Validation Error
```bash
curl -X POST http://localhost:8080/api/v1/categories \
  -H "Content-Type: application/json" \
  -d '{"name": "E"}'
```

**Response (`400 Bad Request`):**
```json
{
  "success": false,
  "error": {
    "code": "BAD_REQUEST",
    "message": "invalid input data: field 'name' must be at least 2 characters"
  }
}
```

---

## ⚡ Event-Driven Architecture (Watermill)

The boilerplate includes an in-memory event bus built on `ThreeDotsLabs/watermill`.

### Publishing an Event
Inside any slice handler:
```go
const TopicOrderPlaced = "order.placed"

type OrderPlacedEvent struct {
    OrderID int64   `json:"order_id"`
    Total   float64 `json:"total"`
}

// Publish domain event
_ = h.bus.Publish(TopicOrderPlaced, OrderPlacedEvent{
    OrderID: order.ID,
    Total:   order.Total,
})
```

### Subscribing to an Event
In `cmd/api/main.go` or within another consumer slice:
```go
_ = bus.Subscribe(ctx, "order.placed", func(ctx context.Context, payload []byte) error {
    var evt OrderPlacedEvent
    if err := json.Unmarshal(payload, &evt); err != nil {
        return err
    }

    // Execute decoupled background tasks:
    // - Dispatch to Kitchen Display System (KDS)
    // - Send thermal receipt print command
    // - Accumulate loyalty reward points
    return nil
})
```


### ⚠️ Delivery Guarantees — Read This Before Trusting an Event

The bus is **in-process and non-persistent** (`gochannel` with
`Persistent: false`). Events live only in memory, in this process:

- A crash, a `SIGKILL`, or a deploy between the database commit and the
  subscriber running **loses the event permanently**. There is no retry, no
  dead-letter queue, no replay.
- Publishing happens *after* the transaction commits, so a failed publish leaves
  the database correct but the side-effect never fires. `create_category.go`
  deliberately logs and swallows this: an unroutable event must not fail a
  request that already succeeded.
- Nothing crosses a process boundary. Scale to more than one replica and each
  instance only sees its own events.

That is the right trade-off for side-effects you can afford to lose — logging,
cache warming, a nice-to-have notification. It is **not** safe for anything the
business depends on: decrementing stock, charging a card, sending a receipt.

**When you need real guarantees, add the transactional outbox pattern:**

1. Add an `outbox` table (`id`, `topic`, `payload JSONB`, `created_at`,
   `published_at NULL`).
2. Inside the same `database.WithTx` that writes your domain rows, `INSERT` the
   event into `outbox`. The commit now makes the state change and the intent to
   publish atomic — the exact failure window described above disappears.
3. Run a background relay that polls unpublished rows
   (`SELECT ... WHERE published_at IS NULL ORDER BY id FOR UPDATE SKIP LOCKED`),
   publishes each to the bus, and stamps `published_at`.
4. Consumers must be **idempotent**: the relay guarantees at-least-once
   delivery, so a redelivery after a crash has to be a no-op. Key handlers on
   the event id.

Swapping `gochannel` for Kafka/NATS/Redis later is a one-line change in
`eventbus.New` — Watermill keeps the `Publish`/`Subscribe` API identical — but
it does **not** remove the need for the outbox. The gap between "row committed"
and "message sent to the broker" is exactly the same gap; only the outbox closes
it.

---

## 🔒 Transactions

A single sqlc query is already atomic, so call it directly. The moment a slice
performs **several writes that must succeed or fail together** — placing an order
writes the order, its line items, and decrements stock — wrap them in
`database.WithTx`:

```go
func (h *PlaceOrderHandler) Handle(ctx context.Context, cmd PlaceOrderCommand) (*Response, error) {
    var placed sqlc.Order

    err := database.WithTx(ctx, h.db, func(q *sqlc.Queries) error {
        order, err := q.CreateOrder(ctx, sqlc.CreateOrderParams{TableID: cmd.TableID})
        if err != nil {
            return fmt.Errorf("create order: %w", err)
        }

        for _, line := range cmd.Lines {
            if err := q.AddOrderLine(ctx, sqlc.AddOrderLineParams{
                OrderID:   order.ID,
                ProductID: line.ProductID,
                Quantity:  line.Quantity,
            }); err != nil {
                return fmt.Errorf("add order line: %w", err)
            }

            if err := q.DecrementStock(ctx, line.ProductID); err != nil {
                return fmt.Errorf("decrement stock: %w", err)
            }
        }

        placed = order
        return nil
    })
    if err != nil {
        return nil, err
    }

    res := toResponse(placed)
    return &res, nil
}
```

**Rules of the helper:**

- Returning `nil` from the callback commits; returning an error rolls back and
  returns *your* error unwrapped by the helper, so `response.ErrConflict` and
  friends still map to the right status code.
- The `*sqlc.Queries` passed in is bound to the transaction. Do not capture it
  outside the callback — it is invalid once `WithTx` returns.
- A panic inside the callback rolls the transaction back and then re-panics, so
  a bug can never strand an open transaction holding row locks.
- Assign results to variables declared *outside* the callback (`placed` above);
  the callback only returns an `error`.
- `WithTxOptions` takes a `*sql.TxOptions` when you need a stricter isolation
  level, e.g. `&sql.TxOptions{Isolation: sql.LevelSerializable}` for logic that
  must not observe a phantom read.

Slices that need transactions take a `database.Beginner` (the one-method
interface `BeginTx` lives on) instead of `*sql.DB`, keeping them as mockable as
the consumer-defined store interfaces.

---

## 🧩 How to Add a New Vertical Slice

Adding a new feature (e.g. `Product` or `Order`) requires **zero modifications** to existing slices:

### Step 1: Add Migration
Create `internal/database/migrations/000002_create_products.sql`:
```sql
CREATE TABLE IF NOT EXISTS products (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    category_id BIGINT NOT NULL REFERENCES categories(id),
    name VARCHAR(255) NOT NULL,
    price NUMERIC(12, 2) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### Step 2: Define Queries & Generate Code
Create `sql/queries/products.sql`:
```sql
-- name: CreateProduct :one
INSERT INTO products (category_id, name, price, is_active)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListProducts :many
SELECT * FROM products ORDER BY name ASC;
```
Run code generation:
```bash
make sqlc
```

### Step 3: Implement Slice Package
Create folder `internal/product/`:
- `create_product.go`: `CreateCommand`, `CreateHandler`, and `HandleHTTP`.
- `list_products.go`: `ListQuery`, `ListHandler`, and `HandleHTTP`.
- `dto.go`: Response models.
- `routes.go`: `RegisterRoutes(g *echo.Group)`.

### Step 4: Register in `cmd/api/main.go`
```go
productSlices := product.NewSlices(queries, bus)
productSlices.RegisterRoutes(v1)
```

If a handler in the new slice writes several tables at once, pass `db` alongside
`queries` and use `database.WithTx` inside the handler — see
[Transactions](#-transactions).

---

## 🧰 Makefile Commands

| Command | Description |
| :--- | :--- |
| `make docker-up` | Start background PostgreSQL container (`cafe-pos-db`) |
| `make docker-down` | Stop background PostgreSQL container |
| `make docker-logs` | Stream PostgreSQL logs |
| `make db-wait` | Block until PostgreSQL accepts connections |
| `make run` | Start the API server |
| `make build` | Compile the binary into `bin/api` |
| `make test` | Run fast, isolated in-memory unit tests (`-race`) |
| `make test-integration`| Run integration tests against `cafe_pos_test` |
| `make test-all` | Run both unit and integration test suites |
| `make coverage` | Calculate statement test coverage |
| `make fmt` | Format all Go source files with `gofmt` |
| `make vet` | Run standard Go static code analysis |
| `make lint` | Run `golangci-lint` (installed on demand) |
| `make vuln` | Scan dependencies with `govulncheck` |
| `make check` | Run every gate CI enforces, before pushing |
| `make help` | List all available targets |
| `make sqlc` | Generate type-safe database queries from SQL |
| `make swagger` | Generate Swagger UI / OpenAPI documentation |
| `make tidy` | Run `go mod tidy` to clean up dependencies |
| `make clean` | Clean build artifacts and local test databases |

---

## 🗺️ Deliberately Not Included

This is a **boilerplate**, not a finished POS. The following are left out on
purpose so the template stays small and unopinionated — each is a decision the
consuming project should make for itself:

| Not included | Why, and what to do about it |
| :--- | :--- |
| **Authentication / authorization** | Every product wants a different scheme (JWT, session, OIDC, API key) and a different role model. Add it as Echo middleware on the `/api/v1` group in `main.go`; the slices need no changes. |
| **Transactional outbox** | See the section above. Add it when an event carries business meaning, not before. |
| **Pagination** | `ListCategories` returns every row, which is fine for a handful of categories. Do **not** copy that shape into `products` or `orders` — add `LIMIT`/`OFFSET` or keyset pagination to those queries from day one. |
| **Rate limiting, body limits, security headers, request timeouts** | Echo ships `middleware.RateLimiter`, `BodyLimit`, `Secure`, and `TimeoutWithConfig`. Wire the ones your deployment needs next to the existing middleware stack. |
| **Metrics and tracing** | Only structured logging (`slog`) is set up. Add OpenTelemetry or Prometheus when you have somewhere to send the data. |
| **Down migrations** | The embedded runner applies forward-only migrations and records them in `schema_migrations`. Roll forward with a new numbered file; if you need reversible migrations, swap in `golang-migrate`. |
| **Multi-tenancy, i18n, soft deletes** | Domain decisions, not infrastructure. |

---

## 📄 License

This boilerplate is open-source and available under the [MIT License](LICENSE).
