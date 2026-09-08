# POS Cafe Backend - Idiomatic Go Vertical Slice Starter

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
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
- **Zero-Setup Auto-Migrations:** Schema migrations are embedded directly into the binary via Go's standard `embed.FS` and applied on startup.
- **Interactive Swagger UI:** Built-in OpenAPI documentation generated with `swaggo/swag`, available out of the box at `/swagger/index.html`.

---

## 🛠️ Tech Stack

| Component | Technology | Description |
| :--- | :--- | :--- |
| **Language** | Go 1.22+ | Modern Go features |
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
│   ├── response/                   # Standardized JSON response envelope & HTTP error mapper
│   │   └── response.go
│   └── validator/                  # Custom Echo validator adapter
│       └── validator.go
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
- **Go**: Version 1.22 or higher.
- **sqlc** (optional, only needed when editing SQL queries):
  ```bash
  go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
  ```
- **swag** (optional, only needed when updating Swagger comments):
  ```bash
  go install github.com/swaggo/swag/cmd/swag@latest
  ```

### Quick Run

```bash
# 1. Clone or navigate to the project directory
cd pos-cafe

# 2. Run the application
make run
# or: go run cmd/api/main.go
```

The application will:
1. Automatically read `.env` (fallback to system environment variables).
2. Connect to PostgreSQL via `DATABASE_URL` (configured in `.env`).
3. Automatically execute embedded SQL migrations.
4. Start the HTTP server at `http://localhost:8080`.

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
# or: TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -v -race -tags=integration ./...
```

> [!IMPORTANT]
> **Zero-Risk Test Safety Guard:** The integration test runner strictly requires `TEST_DATABASE_URL` and enforces that the target database name ends with `_test` (e.g. `cafe_pos_test`). It **never** falls back to your development database and will instantly abort if a non-test database is provided.

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

---

## 🧰 Makefile Commands

| Command | Description |
| :--- | :--- |
| `make docker-up` | Start background PostgreSQL container (`cafe-pos-db`) |
| `make docker-down` | Stop background PostgreSQL container |
| `make docker-logs` | Stream PostgreSQL logs |
| `make run` | Start the API server |
| `make build` | Compile the binary into `bin/api` |
| `make test` | Run fast, isolated in-memory unit tests (`-race`) |
| `make test-integration`| Run integration tests against `cafe_pos_test` |
| `make test-all` | Run both unit and integration test suites |
| `make coverage` | Calculate statement test coverage |
| `make fmt` | Format all Go source files with `gofmt` |
| `make vet` | Run standard Go static code analysis |
| `make sqlc` | Generate type-safe database queries from SQL |
| `make swagger` | Generate Swagger UI / OpenAPI documentation |
| `make tidy` | Run `go mod tidy` to clean up dependencies |
| `make clean` | Clean build artifacts and local test databases |

---

## 📄 License

This boilerplate is open-source and available under the [MIT License](LICENSE).
