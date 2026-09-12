# Task 3 Report: Transactional Authorization, Idempotency, And Audit Executor

## Status: ✅ Complete

## Commits

- `1f9dd20` — `feat(catalog): add transactional command executor`

## Files Created/Modified

| File | Action |
|------|--------|
| `internal/catalog/executor.go` | Created (321 lines) |
| `internal/catalog/executor_integration_test.go` | Created (746 lines) |
| `sql/queries/auth.sql` | Modified (+16 lines: ExpireSession, DisableIdentity) |
| `internal/database/sqlc/auth.sql.go` | Regenerated |
| `internal/database/sqlc/querier.go` | Regenerated |

## Test Summary

**17/17 integration tests pass** (with `-race`):

| Test | Result |
|------|--------|
| `TestExecuteMutation_Success` | ✅ |
| `TestExecuteMutation_SessionNotFound` | ✅ |
| `TestExecuteMutation_SessionRevoked` | ✅ |
| `TestExecuteMutation_SessionExpired` | ✅ |
| `TestExecuteMutation_IdentityDisabled` | ✅ |
| `TestExecuteMutation_MissingCapabilities` | ✅ |
| `TestExecuteMutation_InvalidManagerPin` | ✅ |
| `TestExecuteMutation_ExactReplay` | ✅ |
| `TestExecuteMutation_ConflictingReuse` | ✅ |
| `TestExecuteMutation_CorruptedStoredResult` | ✅ |
| `TestExecuteMutation_AuthorizationBeforeReplay` | ✅ |
| `TestExecuteMutation_AuditFailureRollback` | ✅ |
| `TestExecuteMutation_DenialEventCommitted` | ✅ |
| `TestExecuteMutation_ConcurrentDuplicate` | ✅ |
| `TestExecuteRead_Success` | ✅ |
| `TestExecuteRead_Forbidden` | ✅ |
| `TestExecuteRead_SessionNotFound` | ✅ |

## Implementation Details

### Types
- `Actor` — StaffID + SessionID
- `MutationSpec` — RequestID, Operation, Fingerprint (dedicated struct, never contains PIN), Required capabilities, ManagerPIN, RequireManagerPIN flag
- `AuditRecord` — EventType + Details
- `Runner` — db + queries wrapper
- `committedDenial` — wraps a denial error that was committed (not rolled back)

### Transaction Flow (ExecuteMutation)
1. Begin transaction
2. Reload current identity, session, roles, capabilities (with expired/revoked/locked/disabled checks)
3. Verify required capabilities → FORBIDDEN if missing
4. Verify fresh Manager PIN when price-sensitive → INVALID_MANAGER_PIN if wrong
5. Compute request hash (operation + fingerprint, PIN excluded)
6. Advisory lock on (actor_id XOR request_id)
7. Check existing idempotency record → replay or REQUEST_CONFLICT
8. Execute mutation callback
9. Insert Audit Event (failure → rollback everything)
10. Store idempotent result via ClaimCatalogRequest
11. Commit

### Read Flow (ExecuteRead)
1. Begin read-only repeatable-read transaction
2. Reload authority + verify capability
3. Execute read callback
4. Commit

### Denial Handling
- Denial events (FORBIDDEN, UNAUTHORIZED, INVALID_MANAGER_PIN) are committed as `catalog.denial` audit events
- The `committedDenial` error type signals the executor to commit the transaction before returning the domain error

### Concurrency
- `pg_advisory_xact_lock` serializes concurrent duplicate requests for the same actor+request pair
- Advisory lock is acquired BEFORE idempotency check to prevent race conditions
- Second goroutine replays the stored result without executing the mutation callback

## Report Path

`/home/laffy/Desktop/go-vertical-slice-template-main/pos-cafe/.superpowers/sdd/2026-09-10-catalog-slice/task-3-report.md`

## Fix Round 1 (2026-09-10)

### Status

Complete. All eight reviewer findings were confirmed and corrected. The controller warning about generic `any` values was intentionally not addressed with reflection; dedicated handler fingerprint and audit-detail structs remain the approved enforcement point.

### Commit

- `1c34ac0` - `fix(catalog): harden transactional executor`

### Files Changed

| File | Changes |
|---|---|
| `internal/catalog/executor.go` | Enforced inactivity and implicit price capability; audited reload denials; propagated fingerprint marshal errors; claimed idempotency before mutation; removed audit log-and-return behavior. |
| `internal/catalog/executor_integration_test.go` | Added/reworked authority, denial, marshal, rollback, and true two-connection concurrency coverage. |
| `sql/queries/catalog.sql` | Added `StoreCatalogRequestResult` so the request can be claimed before mutation and completed after audit. |
| `internal/database/sqlc/catalog.sql.go` | Regenerated. |
| `internal/database/sqlc/querier.go` | Regenerated. |

### Review Findings And Coverage

| Finding | Covering test(s) | Resolution |
|---|---|---|
| Inactivity timeout | `TestExecuteMutation_InactiveSessionDenied` | Uses the current workspace with `auth.GetInactivityTimeout` and rejects stale `LastHumanActivityAt` before role loading. |
| Implicit price capability | `TestExecuteMutation_ManagerPINImplicitlyRequiresPriceCapability` | `RequireManagerPIN` now checks current `catalog.change_price` before PIN verification and replay. |
| Reload denial auditing | `TestExecuteMutation_SessionNotFound`, `TestExecuteMutation_SessionRevoked`, `TestExecuteMutation_SessionLocked`, `TestExecuteMutation_DenialEventCommitted` | Expected security denials insert and commit `catalog.authorization_denied`; infrastructure failures still roll back. Unknown authority uses null actor/session audit IDs. |
| Real concurrent overlap | `TestExecuteMutation_ConcurrentDuplicate` | Uses four available connections, blocks the first callback, and observes the exact second advisory lock request waiting in `pg_locks`; callback count is exactly one. |
| Real audit failure rollback | `TestExecuteMutation_AuditFailureRollback` | A test trigger raises from `InsertAuditEvent` after the callback inserts a real category; the category and pre-mutation idempotency claim both roll back. |
| Fingerprint marshal errors | `TestExecuteMutation_FingerprintMarshalFailure` | Hashing returns JSON errors and aborts before callback and request persistence. |
| Revoked versus locked | `TestExecuteMutation_SessionRevoked`, `TestExecuteMutation_SessionLocked` | Revocation uses `RevokeSession`; locking separately uses `UpdateSessionState(...locked)`. |
| Single error handling | `TestExecuteMutation_AuditFailureRollback` | Captures the default logger and proves audit insert errors are returned without executor logging. |

### RED Evidence

Command:

```sh
TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/catalog -run 'TestExecuteMutation_(SessionRevoked|SessionLocked|InactiveSessionDenied|ManagerPINImplicitlyRequiresPriceCapability|FingerprintMarshalFailure|AuditFailureRollback|DenialEventCommitted|ConcurrentDuplicate)$' -v -count=1
```

Output summary:

```text
Go test: 1 passed, 7 failed in 1 packages
SessionRevoked/SessionLocked/DenialEventCommitted: expected one authorization denial, got zero
InactiveSessionDenied: expected error, got nil
ManagerPINImplicitlyRequiresPriceCapability: expected error, got nil
FingerprintMarshalFailure: expected error, got nil
AuditFailureRollback: executor emitted a log before returning the audit insert error
ConcurrentDuplicate: passed with the production advisory lock present
```

Pre-mutation claim RED command:

```sh
TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/catalog -run '^TestExecuteMutation_AuditFailureRollback$' -v -count=1
```

Output:

```text
Go test: 0 passed, 1 failed in 1 packages
"idempotency claim missing before mutation: sql: no rows in result set" does not contain "insert audit event"
```

Concurrency mutation check, with the advisory-lock call temporarily removed and then restored:

```sh
TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -race -tags=integration ./internal/catalog -run '^TestExecuteMutation_ConcurrentDuplicate$' -v -count=1
```

Output without the lock:

```text
Go test: 0 passed, 1 failed in 1 packages
TestExecuteMutation_ConcurrentDuplicate: second transaction did not wait on the advisory lock
```

### GREEN Evidence

Targeted corrected behavior:

```sh
TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -tags=integration ./internal/catalog -run 'TestExecuteMutation_(SessionRevoked|SessionLocked|InactiveSessionDenied|ManagerPINImplicitlyRequiresPriceCapability|FingerprintMarshalFailure|AuditFailureRollback|DenialEventCommitted|ConcurrentDuplicate)$' -v -count=1
```

```text
Go test: 8 passed in 1 packages
```

Prescribed executor integration suite:

```sh
make sqlc && TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -race -tags=integration ./internal/catalog -run 'TestExecuteMutation|TestExecuteRead' -v -count=1
```

```text
sqlc generate
Go test: 21 passed in 1 packages
```

Complete Catalog integration package after removing one stale `sugar` fixture from the local test database:

```sh
TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -p 1 -race -tags=integration ./internal/catalog -count=1
```

```text
Go test: 81 passed in 1 packages
```

Unit and vet verification:

```sh
go test ./internal/catalog/... -count=1
go vet ./internal/catalog/...
```

```text
Go test: 59 passed in 1 packages
go vet: exit 0
```
