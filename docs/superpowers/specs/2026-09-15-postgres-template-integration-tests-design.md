# PostgreSQL Template Database Integration Test Design

**Date:** 2026-09-15
**Status:** Approved

## Summary

The integration suite currently serializes all Go packages with `-p 1` because every package connects to the same PostgreSQL database and destructively resets shared tables. Most top-level tests also create and close a connection pool independently, causing repeated pings, migration advisory locks, and migration-table scans.

This design introduces a test-only database harness that maintains one migrated PostgreSQL template database and creates an isolated ephemeral clone for each integration-test package. A package shares one connection pool for its full test process. Packages can then execute concurrently without changing the existing within-package reset and fixture semantics.

The first phase deliberately does not create a database per test or run tests in the same package with `t.Parallel()`. It targets package-level parallelism with a smaller implementation and resource footprint.

## Goals

- Let the `auth`, `catalog`, `database`, `sales`, `shift`, and `tables` integration packages run concurrently against one PostgreSQL instance.
- Run migrations once against a persistent template and clone the resulting schema for each package.
- Reuse one `*sql.DB` pool for the lifetime of each package test process.
- Preserve direct commands such as `go test -race -tags=integration ./internal/sales`.
- Preserve the integration race-detector quality gate on every pull request.
- Provide a faster local integration target without the race detector.
- Retain all existing test assertions, fixture behavior, and database safety checks.
- Support concurrent integration-suite invocations without database-name collisions or cross-run cleanup.

## Non-Goals

- Creating one database clone per top-level test.
- Adding `t.Parallel()` to integration tests in this phase.
- Replacing `TRUNCATE` or redesigning fixtures and seed builders.
- Changing production database startup or migration behavior.
- Removing unit tests from a `go test -tags=integration` invocation. Untagged unit tests remain included by Go and may still run in the integration job.
- Reducing bcrypt cost or bypassing authentication behavior in HTTP integration tests.
- Guaranteeing a specific percentage or duration improvement before-and-after measurements are collected.

## Current State

Integration files use the `integration` build tag and connect through helpers such as `openSalesTestDB`, `openShiftTestDB`, `openTablesTestDB`, and `openExecutorTestDB`. These helpers call `database.Open`, which creates a new pool, pings PostgreSQL, acquires the migration advisory lock, reads the migration table, and checks embedded migrations.

The suite contains approximately 252 top-level integration tests and no integration use of `t.Parallel()`. Database-opening helper calls are repeated throughout the suite. Sales and other packages also execute broad `TRUNCATE ... CASCADE` statements between tests. Because all packages target `cafe_pos_test`, package concurrency would allow one package to erase another package's active fixtures.

The Makefile and CI therefore pass `-p 1`. CI runs unit tests first and then invokes `go test -tags=integration ./...`; the latter also includes untagged unit tests.

## Selected Approach

Add an `internal/testdb` package that provisions a migrated template and a unique package database before `m.Run`, then closes and drops the package database afterward. Each integration package receives one clone and one shared pool through its `TestMain`.

The same harness is used whether tests are launched through Make, CI, or a direct `go test` command. Make and CI optimize orchestration but are not required for correctness.

### Alternatives Considered

#### CI Matrix with One PostgreSQL Service per Package

This provides strong isolation with fewer test-code changes, but it improves only CI, consumes more runner minutes, and leaves local execution serialized. It is not the primary approach.

#### One Clone per Top-Level Test

This would permit within-package `t.Parallel()` but would create and drop hundreds of physical PostgreSQL databases. The additional I/O, connection pressure, and fixture refactoring are not justified before package-level parallelism is measured.

#### Transaction Rollback per Test

An outer transaction cannot reliably isolate this suite because production runners hold `*sql.DB` and open their own transactions. Retrofitting all code paths onto a shared test transaction would be invasive and could stop tests from exercising production transaction boundaries.

## Architecture

### Test Database Harness

`internal/testdb` owns integration database lifecycle. It exposes this package-test entry point:

```go
func Run(m *testing.M, packageName string, bind func(*sql.DB)) int
```

`Run` keeps setup, `m.Run`, and teardown in one place so teardown errors can affect the returned exit code. The harness must not call `os.Exit` internally because doing so would make cleanup and unit testing harder to control.

Each integration package adds a `TestMain` that binds the provisioned pool to a package-level variable:

```go
func TestMain(m *testing.M) {
	os.Exit(testdb.Run(m, "sales", func(db *sql.DB) {
		salesTestDB = db
	}))
}
```

Existing helpers retain their signatures where practical, but return the shared pool and `sqlc.New(pool)` instead of opening and registering cleanup for a new pool. This minimizes churn across test callers.

### Database Names

`TEST_DATABASE_URL` remains the required source of server credentials and the safety boundary. Its database name must still end in `_test`.

Given a base database such as `cafe_pos_test`, the harness uses:

- Template: `cafe_pos_test_template`
- Clone: `cafe_pos_test_<package>_<random-suffix>`

The package name must come from a fixed allowlist containing `auth`, `catalog`, `database`, `sales`, `shift`, and `tables`. The random suffix is generated by the harness. Generated identifiers are restricted to lowercase ASCII letters, digits, and underscores before they are quoted for SQL. User-controlled strings must never be interpolated as PostgreSQL identifiers.

Unique clone names allow two direct or Make-driven test invocations to run concurrently without sharing or dropping each other's databases.

### Maintenance Connection

The harness derives a maintenance DSN from `TEST_DATABASE_URL` by replacing only the database path with `postgres`, preserving credentials, host, port, and connection options. It opens a small maintenance pool used only for advisory locking and database-level DDL. Template preparation reserves one `*sql.Conn` from that pool; lock acquisition, database DDL, and lock release use this same connection so the session-level advisory lock cannot migrate between pooled PostgreSQL sessions.

Errors and logs identify the operation and package but must redact credentials and avoid printing the complete DSN.

### Template Preparation

Template preparation runs under a session-level PostgreSQL advisory lock acquired through the maintenance connection. All test processes use one documented lock key.

While holding the lock, the harness:

1. Creates `<base>_template` from `template0` if it does not exist.
2. Opens the template through the existing `database.Open`, allowing all pending embedded migrations to run.
3. Closes the template pool so no harness connection remains attached to the template.
4. Creates the unique package database with `CREATE DATABASE ... TEMPLATE <base>_template`.
5. Releases the advisory lock.

The template persists between test runs. A later run applies newly added migrations before cloning, so clones always reflect the current embedded migration set. Tests and fixture code never connect to or seed the template.

Holding the lock across template migration and clone creation prevents another test process from connecting to or updating the template while PostgreSQL copies it.

### Package Pool

The package database is already migrated because it was cloned from the template. The harness opens it directly with the registered pgx `database/sql` driver, configures an appropriate test pool, and pings it. It does not call production `database.Open`, avoiding another migration lock and scan for every package.

The pool remains open for the entire package process. Tests in that package remain sequential and continue to use their existing `TRUNCATE`, targeted cleanup, and unique fixture strategies. Production runners still receive a real `*sql.DB` and continue to open real transactions, preserving behavioral coverage.

## Execution Flow

```text
go test starts a package test binary
        |
        v
package TestMain calls internal/testdb.Run
        |
        v
validate TEST_DATABASE_URL and package identifier
        |
        v
connect to postgres maintenance database
        |
        v
acquire shared template advisory lock
        |
        +--> create template if absent
        +--> apply pending migrations to template
        +--> close template connections
        +--> clone unique package database
        |
        v
release advisory lock
        |
        v
open one package pool and bind it to test helpers
        |
        v
m.Run executes tests and existing fixture resets
        |
        v
close package pool
        |
        v
DROP DATABASE clone WITH (FORCE)
```

Different package processes briefly serialize during template preparation and cloning, then execute their tests concurrently against independent databases.

## Error Handling and Cleanup

### Setup Failure

Any validation, connection, lock, migration, clone, or package-pool failure prevents `m.Run` from executing and returns a non-zero exit code. Error messages include the failed operation and package identifier, but no password or full DSN.

Partial setup is cleaned up where possible. If the clone was created but the package pool cannot be opened, the harness attempts to drop the clone before returning.

### Normal Teardown

After `m.Run`, the harness:

1. Closes the package pool.
2. Uses the maintenance connection to execute `DROP DATABASE <clone> WITH (FORCE)`.
3. Closes the maintenance pool.

If tests passed but pool close or clone cleanup fails, the harness returns a non-zero exit code. If tests already failed, their exit code remains authoritative and cleanup errors are reported as additional diagnostics.

### Interrupted Processes

A process killed before teardown may leave an ephemeral clone. Unique naming prevents that clone from blocking future runs. A cleanup target removes only databases matching the harness-owned base/package/suffix convention. It must not remove the base `_test` database or persistent `_test_template` database.

Automatic age-based cleanup is out of scope because PostgreSQL does not expose a reliable database creation timestamp. Cleanup is explicit and prefix-restricted.

## Package Migration

The following integration packages receive a `TestMain` and package-level shared pool:

- `internal/auth`
- `internal/catalog`
- `internal/database`
- `internal/sales`
- `internal/shift`
- `internal/tables`

Their existing open helpers are converted to shared-pool accessors. Per-test calls to `db.Close` are removed from those helpers. Existing test code should otherwise remain unchanged unless a helper currently assumes pool ownership.

The `internal/database` integration tests use `package database_test`, so importing `internal/testdb`, which itself imports the production database package for template migration, does not create an import cycle.

## Makefile Changes

`test-integration` keeps the race detector but removes package serialization:

```make
test-integration:
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race -tags=integration ./...
```

Add a fast local target without race instrumentation:

```make
test-integration-fast:
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -tags=integration ./...
```

The coverage target also removes `-p 1`, because package databases are isolated. A small `internal/testdb/cmd/cleanup` command calls the harness cleanup API, and the Make cleanup target invokes it with `go run ./internal/testdb/cmd/cleanup`. The command removes only stale ephemeral clone names matching the strict generated naming convention.

Direct package commands remain supported:

```bash
TEST_DATABASE_URL="postgres://.../cafe_pos_test?sslmode=disable" \
  go test -race -tags=integration ./internal/sales
```

## CI Changes

Split the current combined build-and-test job into independent unit and integration jobs so they can run concurrently.

The integration job retains one PostgreSQL 17 service and runs:

```bash
go test -v -race -tags=integration \
  -coverprofile=coverage-int.out -covermode=atomic ./...
```

It no longer passes `-p 1`. Go may schedule integration packages concurrently; each package provisions a separate clone on the shared service.

The unit job retains `-race` and unit coverage. The integration command still runs untagged unit tests because Go build tags add integration files rather than exclude ordinary test files. Removing that duplicate execution would require moving integration tests to separate directories or tagging all unit test files with `!integration`; both are excluded from this phase.

Formatting, generated-code verification, vet, build, lint, vulnerability scanning, and Codecov uploads remain quality gates. Job dependencies should not serialize unit and integration execution unless an artifact dependency requires it.

## Test Strategy

### Harness Unit Tests

Unit tests cover pure behavior without PostgreSQL:

- Parsing supported `TEST_DATABASE_URL` values.
- Replacing the database path while preserving query options.
- Rejecting a missing URL, malformed URL, or database without an `_test` suffix.
- Accepting only allowlisted package identifiers.
- Generating unique, valid clone identifiers.
- Distinguishing base, template, and ephemeral clone names.
- Redacting credentials from errors.
- Combining test and cleanup exit outcomes correctly.

### Harness Integration Tests

Database-backed harness tests verify:

- The template contains the current migration records and expected schema.
- Two clones can be created from one template.
- Data written to clone A is not visible in clone B.
- Closing pools permits normal cleanup.
- Both clones can be dropped without modifying the template or base test database.

These tests use the same `integration` build tag and safety checks as the rest of the suite. They live in package `testdb` and exercise the lower-level provisioning functions directly; `internal/testdb` does not define a provisioning `TestMain`, so the harness tests cannot recursively invoke themselves.

### Existing Suite Verification

Verification includes:

```bash
go test -race -tags=integration ./internal/auth
go test -race -tags=integration ./internal/catalog
go test -race -tags=integration ./internal/database
go test -race -tags=integration ./internal/sales
go test -race -tags=integration ./internal/shift
go test -race -tags=integration ./internal/tables
make test-integration-fast
make test-integration
make coverage
```

Two full integration commands are also launched concurrently to prove clone-name and cleanup isolation. Test commands that collect timing are run serially when comparing before and after performance, so machine contention does not invalidate the comparison.

### Performance Evidence

Before implementation changes, record the current full integration duration under controlled local conditions. After implementation, repeat the same command and environment multiple times. Report package-level timing and full-suite wall time without promising a fixed improvement threshold.

Success requires structural evidence in addition to timing:

- No integration command uses `-p 1`.
- Separate package test processes connect to distinct clone names.
- `database.Open` is no longer called for every top-level test.
- All existing integration and concurrency tests pass under `-race`.
- Coverage output is still produced.

## Security and Safety

- `TEST_DATABASE_URL` remains mandatory.
- The configured base database name must end in `_test` before any database DDL is attempted.
- The maintenance database is used only to manage harness-owned test databases.
- Database identifiers are generated from an allowlist and random suffix, validated, and safely quoted.
- Destructive cleanup is limited to the exact clone created by the current harness invocation or to the strict ephemeral-clone prefix in the explicit cleanup target.
- Logs and errors never expose passwords or complete DSNs.
- The base test database and persistent template are never dropped by normal package teardown.

## Rollout

1. Add and test `internal/testdb` lifecycle and naming utilities.
2. Migrate one representative package, preferably `sales`, and verify direct execution plus cleanup.
3. Migrate the remaining integration packages to package-level pools.
4. Remove `-p 1` and add the fast and cleanup Make targets.
5. Split CI unit and integration jobs while retaining `-race`.
6. Collect before-and-after timing evidence and document the result.

If package-level parallelism is still insufficient, a later design may evaluate per-test clones, fixture batching, or bypassing repeated bcrypt work in tests that do not exercise sign-in behavior.

## Acceptance Criteria

- Every integration package uses one ephemeral database clone and one shared connection pool per package process.
- The template is migrated before cloning and is never modified by test fixtures.
- Two suite invocations can run concurrently without sharing or deleting each other's clones.
- Direct package-level `go test -tags=integration` commands continue to work.
- `make test-integration` and integration CI no longer use `-p 1`.
- CI integration tests continue to run with `-race` on pull requests.
- `test-integration-fast` runs the integration suite locally without `-race`.
- Existing unit, integration, concurrency, schema, and coverage checks pass.
- Setup and cleanup failures produce non-zero outcomes without leaking credentials.
- Production database connection and migration behavior remain unchanged.
