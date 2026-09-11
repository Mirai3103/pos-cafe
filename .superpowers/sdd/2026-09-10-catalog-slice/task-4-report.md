# Task 4 Report: Category Commands

## Status: ✅ Complete

## Commits

- `de60ba1` — `feat(catalog): add category commands`

## Files Created/Modified

| File | Action | Description |
|------|--------|-------------|
| `internal/catalog/dto.go` | Created | Defined `CreateCategoryCommand`, `RenameCategoryCommand`, and `CategoryResponse`. |
| `internal/catalog/category_commands.go` | Created | Implemented `CreateCategoryHandler` (201 Created) and `RenameCategoryHandler` (200 OK) executing mutations through `ExecuteMutation` with `catalog.administer_structure`, dedicated fingerprints, row locking, and audit events. |
| `internal/catalog/commands_integration_test.go` | Created | Comprehensive integration tests asserting trimming, case-insensitive conflicts, internal space preservation, UUID results, exact replays, missing category handling, and audit event recording. |
| `internal/catalog/errors.go` | Modified | Added `MapDBError` mapping PostgreSQL code `23505` to `ErrNameConflict` and `sql.ErrNoRows`/`23503` to `ErrNotFound`. |
| `internal/catalog/errors_test.go` | Modified | Added unit tests for `MapDBError`. |

## TDD Evidence

### RED Phase
- **Command executed**:
  `TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -tags=integration ./internal/catalog -run 'TestCreateCategory|TestRenameCategory' -v`
- **Failing output**:
  ```text
  === RUN   TestCreateCategory/Success_Trimming_InternalSpace_UUID_Audit
      commands_integration_test.go:55: 
          Error: Not equal: 
                 expected: 201
                 actual  : 0
      commands_integration_test.go:56: 
          Error: Should not be: uuid.UUID{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}
      commands_integration_test.go:57: 
          Error: Not equal: 
                 expected: "Special   Milk   Tea"
                 actual  : ""
  === RUN   TestCreateCategory/CaseInsensitiveConflict
      commands_integration_test.go:108: 
          Error: An error is expected but got nil.
  === RUN   TestCreateCategory/ExactReplay
      commands_integration_test.go:135: 
          Error: Not equal: 
                 expected: 201
                 actual  : 0
  === RUN   TestCreateCategory/ForbiddenWithoutAdministerStructure
      commands_integration_test.go:166: 
          Error: An error is expected but got nil.
  === RUN   TestRenameCategory/Success_Trimming_InternalSpace_Audit
      commands_integration_test.go:208: 
          Error: Not equal: 
                 expected: 200
                 actual  : 0
  === RUN   TestRenameCategory/ExactReplay
      commands_integration_test.go:303: 
          Error: Not equal: 
                 expected: 200
                 actual  : 0
  === RUN   TestRenameCategory/MissingCategory
      commands_integration_test.go:331: 
          Error: An error is expected but got nil.
  === RUN   TestRenameCategory/ForbiddenWithoutAdministerStructure
      commands_integration_test.go:355: 
          Error: An error is expected but got nil.
  --- FAIL: TestCreateCategory (0.12s)
  --- FAIL: TestRenameCategory (0.13s)
  FAIL
  ```
- **Why failure was expected**: Stubs returned `(0, CategoryResponse{}, nil)` without performing authorization, database mutation, normalization, or audit event recording.

### GREEN Phase
- **Command executed**:
  `make sqlc && TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -count=1 -tags=integration ./internal/catalog -run 'TestCreateCategory|TestRenameCategory' -v`
- **Passing output**:
  ```text
  sqlc generate
  === RUN   TestCreateCategory
  === RUN   TestCreateCategory/Success_Trimming_InternalSpace_UUID_Audit
  === RUN   TestCreateCategory/CaseInsensitiveConflict
  === RUN   TestCreateCategory/ExactReplay
  === RUN   TestCreateCategory/ForbiddenWithoutAdministerStructure
  --- PASS: TestCreateCategory (0.12s)
      --- PASS: TestCreateCategory/Success_Trimming_InternalSpace_UUID_Audit (0.03s)
      --- PASS: TestCreateCategory/CaseInsensitiveConflict (0.02s)
      --- PASS: TestCreateCategory/ExactReplay (0.02s)
      --- PASS: TestCreateCategory/ForbiddenWithoutAdministerStructure (0.02s)
  === RUN   TestRenameCategory
  === RUN   TestRenameCategory/Success_Trimming_InternalSpace_Audit
  === RUN   TestRenameCategory/CaseInsensitiveConflict
  === RUN   TestRenameCategory/ExactReplay
  === RUN   TestRenameCategory/MissingCategory
  === RUN   TestRenameCategory/ForbiddenWithoutAdministerStructure
  --- PASS: TestRenameCategory (0.16s)
      --- PASS: TestRenameCategory/Success_Trimming_InternalSpace_Audit (0.04s)
      --- PASS: TestRenameCategory/CaseInsensitiveConflict (0.03s)
      --- PASS: TestRenameCategory/ExactReplay (0.03s)
      --- PASS: TestRenameCategory/MissingCategory (0.02s)
      --- PASS: TestRenameCategory/ForbiddenWithoutAdministerStructure (0.03s)
  PASS
  ok  	github.com/Mirai3103/pos-cafe/internal/catalog	0.438s
  ```

## Test Summary

| Test | Subtest | Result |
|------|---------|--------|
| `TestCreateCategory` | `Success_Trimming_InternalSpace_UUID_Audit` | ✅ PASS |
| `TestCreateCategory` | `CaseInsensitiveConflict` | ✅ PASS |
| `TestCreateCategory` | `ExactReplay` | ✅ PASS |
| `TestCreateCategory` | `ForbiddenWithoutAdministerStructure` | ✅ PASS |
| `TestRenameCategory` | `Success_Trimming_InternalSpace_Audit` | ✅ PASS |
| `TestRenameCategory` | `CaseInsensitiveConflict` | ✅ PASS |
| `TestRenameCategory` | `ExactReplay` | ✅ PASS |
| `TestRenameCategory` | `MissingCategory` | ✅ PASS |
| `TestRenameCategory` | `ForbiddenWithoutAdministerStructure` | ✅ PASS |
| `TestMapDBError` | Unit tests for error translation | ✅ PASS |

## Implementation Details

### Handlers and Commands
1. **`CreateCategoryHandler`**:
   - Computes display name and normalized key using `NormalizeName(cmd.Name)`.
   - Executes mutation with operation `catalog.category.create` requiring `catalog.administer_structure`.
   - Fingerprint: `createCategoryFingerprint{Name: display}`.
   - Inserts row into `menu_categories` via `q.CreateMenuCategory`.
   - Produces `catalog.category.created` audit event with `category_id` and `name`.
   - Returns status `201 Created` and `CategoryResponse`.

2. **`RenameCategoryHandler`**:
   - Computes display name and normalized key using `NormalizeName(cmd.Name)`.
   - Executes mutation with operation `catalog.category.rename` requiring `catalog.administer_structure`.
   - Fingerprint: `renameCategoryFingerprint{CategoryID: cmd.CategoryID, Name: display}`.
   - Locks row before rename using `q.GetMenuCategoryForUpdate(ctx, cmd.CategoryID)`.
   - If not found, maps `sql.ErrNoRows` to `ErrNotFound`.
   - Updates row via `q.RenameMenuCategory`.
   - Produces `catalog.category.renamed` audit event with `category_id`, `old_name`, and `new_name`.
   - Returns status `200 OK` and `CategoryResponse`.

3. **`MapDBError`**:
   - Translates Postgres unique constraint violation (`Code == "23505"`) to `ErrNameConflict`.
   - Translates foreign key violation (`Code == "23503"`) to `ErrNotFound`.
   - Translates `sql.ErrNoRows` to `ErrNotFound`.

## Self-Review Findings
- **Completeness**: All required types (`CreateCategoryCommand`, `RenameCategoryCommand`, `CategoryResponse`, `CreateCategoryHandler`, `RenameCategoryHandler`) and behaviors are implemented and verified.
- **Security & Privacy**: No PINs or secrets exist in category commands, fingerprints, or audit records. Dedicated structs ensure tight data minimization.
- **Quality & Discipline**: Code follows standard Go conventions, passed `go vet`, and cleanly passed integration tests with `-race` and `-count=1`.

---

## Fix Round 1 (2026-09-11)

### Status: ✅ Complete

### Commits
- `a92c54f` — `fix(catalog): extract domain constants for category commands`

### Reviewer Findings Addressed
1. **Plan-mandated SQL and generated files**:
   - `sql/queries/catalog.sql` and `internal/database/sqlc/*` were already added in Task 1 with complete queries (`CreateMenuCategory`, `RenameMenuCategory`, `GetMenuCategoryForUpdate`, `GetMenuCategoryByID`).
   - Running `make sqlc` executes cleanly and produces zero git diff.
2. **Domain Constants Extracted**:
   - Defined exported domain constants in `internal/catalog/domain.go`:
     - Capabilities: `CapAdministerStructure = "catalog.administer_structure"`, `CapChangePrice`, `CapViewPrices`, `CapManageAvailability`
     - Operations: `OpCategoryCreate = "catalog.category.create"`, `OpCategoryRename = "catalog.category.rename"`
     - Events: `EventCategoryCreated = "catalog.category.created"`, `EventCategoryRenamed = "catalog.category.renamed"`
   - Refactored `internal/catalog/category_commands.go` to use these constants instead of string literals.

### Covering Verification Tests
- **Command executed**:
  `make sqlc && TEST_DATABASE_URL="postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos_test?sslmode=disable" go test -count=1 -tags=integration ./internal/catalog -run 'TestCreateCategory|TestRenameCategory' -v`
- **Output**:
  ```text
  sqlc generate
  === RUN   TestCreateCategory
  === RUN   TestCreateCategory/Success_Trimming_InternalSpace_UUID_Audit
  === RUN   TestCreateCategory/CaseInsensitiveConflict
  === RUN   TestCreateCategory/ExactReplay
  === RUN   TestCreateCategory/ForbiddenWithoutAdministerStructure
  --- PASS: TestCreateCategory (0.14s)
      --- PASS: TestCreateCategory/Success_Trimming_InternalSpace_UUID_Audit (0.04s)
      --- PASS: TestCreateCategory/CaseInsensitiveConflict (0.03s)
      --- PASS: TestCreateCategory/ExactReplay (0.02s)
      --- PASS: TestCreateCategory/ForbiddenWithoutAdministerStructure (0.02s)
  === RUN   TestRenameCategory
  === RUN   TestRenameCategory/Success_Trimming_InternalSpace_Audit
  === RUN   TestRenameCategory/CaseInsensitiveConflict
  === RUN   TestRenameCategory/ExactReplay
  === RUN   TestRenameCategory/MissingCategory
  === RUN   TestRenameCategory/ForbiddenWithoutAdministerStructure
  --- PASS: TestRenameCategory (0.15s)
      --- PASS: TestRenameCategory/Success_Trimming_InternalSpace_Audit (0.04s)
      --- PASS: TestRenameCategory/CaseInsensitiveConflict (0.03s)
      --- PASS: TestRenameCategory/ExactReplay (0.03s)
      --- PASS: TestRenameCategory/MissingCategory (0.02s)
      --- PASS: TestRenameCategory/ForbiddenWithoutAdministerStructure (0.03s)
  PASS
  ok  	github.com/Mirai3103/pos-cafe/internal/catalog	2.263s
  ```

