.PHONY: help run build test test-integration test-all coverage fmt vet lint vuln check sqlc swagger tidy clean \
	docker-up docker-down docker-logs db-wait

# Single source of truth for the integration-test database.
TEST_DATABASE_URL ?= postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable
GOLANGCI_VERSION  ?= v2.13.2

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

## --- Local infrastructure ---

docker-up: ## Start PostgreSQL (creates cafe_pos + cafe_pos_test on first boot)
	docker compose up -d

docker-down: ## Stop PostgreSQL (keeps the data volume)
	docker compose down

docker-logs: ## Tail PostgreSQL logs
	docker compose logs -f postgres

db-wait: ## Block until PostgreSQL is accepting connections
	@echo "waiting for postgres to become healthy..."
	@until docker compose exec -T postgres pg_isready -U cafe_pos -d cafe_pos >/dev/null 2>&1; do sleep 1; done
	@echo "postgres is ready"

## --- Application ---

run: ## Run the API server
	go run ./cmd/api

build: ## Build the API binary into bin/
	go build -o bin/api ./cmd/api

## --- Quality gates ---

fmt: ## Format all Go code
	gofmt -w .

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint (installs it on demand)
	@command -v golangci-lint >/dev/null 2>&1 || \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	golangci-lint run ./...

vuln: ## Scan dependencies for known vulnerabilities
	@command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

test: ## Run unit tests with the race detector
	go test -race ./...

# -p 1 is required, not an optimisation: every integration package TRUNCATEs the
# same tables in the one test database, so running packages in parallel makes
# them wipe each other's fixtures mid-test.
test-integration: ## Run integration tests (needs docker-up)
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race -p 1 -tags=integration ./...

test-all: test test-integration ## Run unit + integration tests

coverage: ## Report test coverage across unit + integration tests
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -cover -p 1 -tags=integration ./...

check: fmt vet lint test ## Everything CI enforces, before you push

## --- Code generation ---

sqlc: ## Regenerate type-safe SQL bindings
	sqlc generate

swagger: ## Regenerate Swagger/OpenAPI docs
	swag init -g cmd/api/main.go -o docs

tidy: ## Tidy go.mod / go.sum
	go mod tidy

clean: ## Remove build artifacts
	rm -rf bin/ *.db*
