# Design Specification: Catalog Slice (`internal/catalog`)

- **Author:** OpenCode & Team
- **Date:** 2026-09-10
- **Status:** Approved
- **Phase:** Phase 2, Catalog Expansion

---

## 1. Purpose

This specification defines the Go implementation of the existing TypeScript Catalog business behavior. The implementation may improve structure and correctness, but it must preserve the canonical business rules in `/home/laffy/cafe-pos/CONTEXT.md` and the opening-day menu specification.

When TypeScript runtime behavior conflicts with those documents, the canonical documents win. Known implementation defects are corrected rather than migrated.

Phase 2 replaces the disposable `internal/category` reference slice. Its routes, schema, generated queries, and tests have no compatibility requirement.

### Goals

1. Implement categories, items, Sizes, Modifier Groups, Modifier Options, inheritance, Availability, and Retirement as one Catalog consistency boundary.
2. Preserve direct-versus-sized pricing and whole-VND price behavior.
3. Provide separate sellable, management, and availability projections so callers receive only authorized information.
4. Recheck current authority inside each operation transaction.
5. Require a fresh Manager PIN for price-sensitive commands.
6. Make every mutation atomically idempotent and every successful state change auditable.
7. Expose stable REST contracts for the later frontend and Sales migrations.

### Non-Goals

- Backward compatibility with `/api/v1/categories`.
- Generic CRUD or destructive deletion.
- Moving items between categories.
- Converting an existing item between direct and sized pricing.
- Adding or removing Sizes after item creation.
- Adding or removing Modifier Options after group creation.
- Changing Modifier Group min/max after creation.
- Detaching assignments, removing exclusions, restoring retired entities, or reusing retired identities.
- Sales drafts, Commit-time revalidation, commercial snapshots, or historical order behavior. Those remain Phase 5 responsibilities.
- Reliable external event delivery. A transactional outbox may be added when a concrete integration requires one.

---

## 2. Authority And Terminology

The implementation uses the terms defined in `/home/laffy/cafe-pos/CONTEXT.md`:

- A Menu Item belongs to exactly one Menu Category.
- An item is priced directly or through a required Size choice, never both.
- A Size carries an absolute selling price, not an adjustment.
- A Modifier Option surcharge is non-negative.
- A Modifier Group is reusable and may be inherited from a category or attached directly to an item.
- Availability is temporary eligibility for new work.
- Retirement is permanent removal from future selection while retaining identity and history.

The words `Size`, `Availability`, and `Retirement` retain their domain capitalization in documentation. Go identifiers use normal exported naming.

---

## 3. Architecture

`internal/catalog` replaces `internal/category` and owns the complete Catalog consistency boundary. It is one package because sellability, inheritance, authorization, auditing, and mutation atomicity cross individual entity types.

The package is organized by behavior:

- Category creation and rename.
- Item creation, rename, reprice, Availability, and Retirement.
- Size rename, reprice, Availability, and Retirement.
- Modifier Group creation, rename, defaults, assignment, inheritance exclusion, and Retirement.
- Modifier Option rename, reprice, Availability, and Retirement.
- Sellable, management, availability, modifier-management, and audit reads.
- Shared domain validation, transactional command execution, authorization, fingerprinting, and DTO assembly.

Each handler owns a narrow, consumer-defined interface containing only the generated sqlc operations it uses. API DTOs remain separate from generated database models.

General transaction mechanics remain in `internal/database`. Catalog transaction orchestration and Catalog request fingerprints remain in `internal/catalog`. Existing auth idempotency code may be extracted only when the result is genuinely domain-neutral; Catalog must not import an auth-owned idempotency helper.

### Command Flow

Every mutation executes this sequence:

```text
begin transaction
-> reload current identity, session, roles, and capabilities
-> verify required capabilities
-> verify fresh Manager PIN when price-sensitive
-> claim or replay actor-scoped request_id
-> lock affected Catalog rows in deterministic order
-> validate current business state
-> apply mutation
-> insert authoritative Audit Event
-> store idempotent result
-> commit
```

### Read Flow

Every read reloads current authority and assembles its projection in a read-only, repeatable-read transaction. Multiple focused SQL queries and deterministic Go assembly are acceptable. The roadmap's one-or-two-query target does not override consistency, maintainability, or information security.

---

## 4. Database Design

The Phase 2 migration removes the boilerplate `categories` table and creates the authoritative Catalog schema. No data migration or compatibility view is required.

All Catalog entity IDs are UUIDs generated by PostgreSQL. All timestamps are `TIMESTAMPTZ`. Prices and surcharges are `BIGINT` whole-VND values.

### 4.1 `menu_categories`

- `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
- `name TEXT NOT NULL`
- `normalized_name TEXT NOT NULL UNIQUE`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`

Categories have no description, display order, Availability, Retirement, update timestamp, or destructive delete operation.

### 4.2 `menu_items`

- UUID identity and mandatory category foreign key.
- Display and normalized names, unique by `(category_id, normalized_name)`.
- Nullable `price_vnd BIGINT` for direct pricing.
- `available BOOLEAN NOT NULL DEFAULT true`.
- Retirement timestamp, reason, and optional note.
- Creation and update timestamps.

A direct price is an integer from 1 through `2_147_483_647` VND. A null direct price denotes sized pricing and requires one or more Size rows created in the same transaction.

### 4.3 `menu_item_sizes`

- UUID identity and mandatory item foreign key.
- Display and normalized names, unique by `(menu_item_id, normalized_name)`.
- Absolute `price_vnd BIGINT NOT NULL` from 1 through `2_147_483_647`.
- Independent Availability.
- Retirement metadata and timestamps.

### 4.4 `modifier_groups`

- UUID identity.
- Display and globally unique normalized name.
- `min_selections` greater than or equal to zero.
- `max_selections` greater than or equal to one.
- `min_selections <= max_selections`.
- Retirement metadata and timestamps.

### 4.5 `modifier_options`

- UUID identity and mandatory group foreign key.
- Display and normalized names, unique by `(modifier_group_id, normalized_name)`.
- `surcharge_vnd BIGINT NOT NULL` from zero through `2_147_483_647`.
- Independent Availability.
- Retirement metadata and timestamps.

### 4.6 Association Tables

- `item_modifier_groups`: direct item assignments.
- `category_modifier_groups`: category defaults inherited by their items.
- `item_modifier_group_exclusions`: item exclusions of inherited category groups.
- `modifier_group_default_options`: explicit defaults for a group.

Each table uses a composite primary key and foreign keys. Assignment rows are retained when an entity is retired. They are removed from future-selection projections by lifecycle rules rather than destructive cleanup.

### 4.7 Idempotency

`catalog_mutation_requests` contains:

- `actor_id UUID`.
- `request_id UUID`.
- Stable operation name.
- SHA-256 hash of normalized business input.
- HTTP response status.
- Serialized response body.
- Creation timestamp.

The primary key is `(actor_id, request_id)`. Operation and input hash are compared during replay. Manager PIN values are never included in the hash or stored result.

### 4.8 Audit Events

`audit_events` stores:

- UUID event identity.
- Stable event type.
- Actor identity and access-session identity when known.
- JSONB business details.
- Occurrence timestamp.

Every successful Catalog state change, including category changes, inserts an event in the same transaction. An idempotent replay or a successful same-state Availability no-op does not insert a duplicate business event. Security-relevant denials are recorded without persisting business effects. Event details must not contain PINs, session tokens, request headers, or complete request payloads.

### 4.9 Name Normalization

Display names are trimmed. Uniqueness keys are the Unicode lowercase form of the trimmed value. Internal whitespace is preserved because canonical rules specify case-insensitive comparison and surrounding-whitespace removal, not whitespace collapsing.

Normalization occurs in Go before persistence; unique indexes on `normalized_name` are the race-safe authority.

### 4.10 Retirement Constraints

Retirement reasons are:

- `NO_LONGER_OFFERED`
- `MENU_RESTRUCTURE`
- `OTHER`

`OTHER` requires a non-empty trimmed note. Notes are limited to 500 characters. Database checks enforce consistent null/non-null Retirement fields, while Go validates the reason-specific note rule.

---

## 5. Business Rules

### 5.1 Item Pricing

Item creation accepts exactly one pricing form:

- A positive direct `price_vnd` and no Sizes.
- A null direct price and one or more Sizes with positive absolute prices.

Duplicate normalized Size names are rejected before insertion and by the database. The pricing form cannot be converted later. Repricing changes a direct item price, Size price, or Modifier Option surcharge without changing structure.

### 5.2 Modifier Groups

A group is created with at least one option. Option names are unique within the group. `max_selections` cannot exceed the initial option count.

An explicit default set is optional. When supplied, it must:

- Contain distinct options.
- Refer only to current, available options in that group.
- Contain between `min_selections` and `max_selections` options.

Replacing defaults follows the same rules. Later Availability or Retirement changes may make persisted defaults unavailable; read projections filter those defaults rather than silently selecting invalid choices.

### 5.3 Inheritance

Effective groups are computed as:

```text
(current category assignments - inherited exclusions)
+ current direct item assignments
```

A group reached through category and direct assignment applies once. Excluding an inherited group does not suppress a separate direct assignment of the same group.

An exclusion may be created only when the item belongs to a category currently assigned to the group. Assignments and exclusions involving retired participants are rejected.

### 5.4 Availability

Availability applies only to items, Sizes, and Modifier Options. Setting the current value again is a successful no-op: it returns current state, does not update timestamps, and does not emit another business Audit Event.

Child Availability cannot change when its parent item or group is retired. Making a Size or option unavailable may make items unsellable; it does not alter existing historical records.

### 5.5 Retirement

Retirement applies only to items, Sizes, Modifier Groups, and Modifier Options. It is permanent and has no restore command.

Retired entities:

- Remain visible in management and modifier-management projections.
- Are omitted from future-selection and availability projections.
- Cannot be renamed, repriced, assigned, excluded, or have Availability changed.
- Preserve association and future historical references.

Retired groups are omitted from effective future choices. Their retained assignment rows do not make attached items unsellable.

### 5.6 Sellability

An item is sellable only when:

1. It is current and available.
2. It has exactly one valid pricing form.
3. A sized item has at least one current, available Size.
4. Every current effective group with a positive minimum has at least that many current, available options.

Sellability is a projection rule. Phase 5 must independently revalidate the same current facts at Commit before creating immutable commercial snapshots.

---

## 6. Authorization

Routes require an authenticated, active Staff Access Session. Echo middleware may reject obviously unauthenticated requests, but business handlers reload current identity, session, roles, and capabilities inside their transaction.

Capability mapping:

| Operation | Required capability |
| --- | --- |
| Sellable menu | `catalog.view_prices` |
| Management menu | `catalog.view_prices` |
| Modifier-management read | `catalog.view_prices` |
| Availability menu | `catalog.manage_availability` |
| Availability mutations | `catalog.manage_availability` |
| Structure, naming, assignment, defaults, exclusions, Retirement | `catalog.administer_structure` |
| Audit read | `audit.inspect` |

Creating an item, creating a Modifier Group with priced options, and repricing an item, Size, or Modifier Option additionally require `catalog.change_price` and a fresh Manager PIN.

Fresh-PIN verification uses the authenticated actor's Manager identity. It occurs before idempotent replay. A caller whose authority was removed cannot replay an earlier successful request.

---

## 7. REST API

All routes are under `/api/v1/catalog` and use the existing `{success,data,error}` response envelope.

### 7.1 Reads

| Method and path | Capability | Result |
| --- | --- | --- |
| `GET /menu/sellable` | `catalog.view_prices` | Sellable priced menu |
| `GET /menu/manage` | `catalog.view_prices` | Complete management menu |
| `GET /menu/availability` | `catalog.manage_availability` | Current price-free availability menu |
| `GET /modifier-groups` | `catalog.view_prices` | Complete group/option management view |
| `GET /audit-events` | `audit.inspect` | Newest-first Catalog Audit Events |

### 7.2 Category Commands

- `POST /categories`: create category.
- `PATCH /categories/:category_id/name`: rename category.

### 7.3 Item Commands

- `POST /items`: create a direct-priced or sized item.
- `PATCH /items/:item_id/name`: rename item.
- `PATCH /items/:item_id/price`: reprice direct item.
- `PATCH /items/:item_id/availability`: set item Availability.
- `POST /items/:item_id/retirement`: retire item.

### 7.4 Size Commands

- `PATCH /sizes/:size_id/name`: rename Size.
- `PATCH /sizes/:size_id/price`: reprice Size.
- `PATCH /sizes/:size_id/availability`: set Size Availability.
- `POST /sizes/:size_id/retirement`: retire Size.

### 7.5 Modifier Commands

- `POST /modifier-groups`: create a group with its initial options and optional defaults.
- `PATCH /modifier-groups/:group_id/name`: rename group.
- `PUT /modifier-groups/:group_id/defaults`: replace defaults.
- `POST /modifier-groups/:group_id/retirement`: retire group.
- `PATCH /modifier-options/:option_id/name`: rename option.
- `PATCH /modifier-options/:option_id/price`: reprice option surcharge.
- `PATCH /modifier-options/:option_id/availability`: set option Availability.
- `POST /modifier-options/:option_id/retirement`: retire option.

### 7.6 Assignment Commands

- `POST /items/:item_id/modifier-groups/:group_id`: attach group directly to item.
- `POST /categories/:category_id/modifier-groups/:group_id`: attach group as category default.
- `POST /items/:item_id/inherited-modifier-group-exclusions/:group_id`: exclude inherited group.

Every mutation body contains `request_id`. Price-sensitive bodies additionally contain `manager_pin`. Identifiers already present in the route are not duplicated in the body.

---

## 8. Projection Contracts

### 8.1 Sellable Menu

Returns only sellable items and current selectable choices. It includes category identity/name, item identity/name/direct price, current available Sizes with absolute prices, effective current groups, current available options with surcharges, and valid visible default option IDs.

Categories with no sellable items are omitted.

### 8.2 Management Menu

Returns all categories and current or retired items. It includes:

- Direct and Size prices.
- Item, Size, and option Availability.
- Retirement state and metadata.
- Category assignments.
- Direct item assignments.
- Effective item groups.
- Inherited exclusions.
- Group defaults.

Empty categories are retained.

### 8.3 Availability Menu

Returns current categories, items, Sizes, groups, and options needed to manage temporary Availability. It excludes:

- All prices and surcharges.
- Retirement metadata.
- Defaults.
- Assignment-source details.
- Exclusion details.

Retired entities are omitted. Empty categories remain visible so current items can be administered consistently.

### 8.4 Modifier Management

Returns current and retired Modifier Groups and Options, including bounds, defaults, prices, Availability, and Retirement metadata.

### 8.5 Ordering

All projections use ascending normalized names and UUIDs as deterministic tie-breakers. Display-order fields are not introduced because they are not part of the canonical Catalog behavior.

---

## 9. Transactions And Concurrency

Mutation transactions use row locks for affected entities and parents. Locks are acquired in a documented deterministic order to reduce deadlocks.

Friendly uniqueness checks may provide domain-specific errors. PostgreSQL unique constraints remain authoritative for races.

The idempotency sequence is:

1. Authorize current actor and verify fresh PIN if required.
2. Claim `(actor_id, request_id)`.
3. Compare operation and normalized-input hash.
4. Replay status and body for an exact match.
5. Return `REQUEST_CONFLICT` for different reuse.
6. For a new request, mutate, audit, and store the result atomically.

Failed operations are not cached. Concurrent identical requests cannot execute the mutation twice.

Read handlers use read-only repeatable-read transactions so capability checks and hierarchy queries observe one database snapshot.

---

## 10. Audit And Notifications

The Audit Event row is part of the business transaction. Audit insertion failure rolls back the Catalog mutation and idempotency claim.

Events contain the actor, session, operation-specific target and before/after facts, request ID where useful, and occurrence time. Secrets are prohibited.

Authorization and fresh-PIN denials that require audit evidence are committed as denial-only events. The transaction helper must support committing such an event while returning the corresponding domain error and no business result.

Watermill may publish optional post-commit notifications. The in-memory bus is not authoritative, and failure to publish a secondary notification does not reverse committed Catalog state.

---

## 11. Errors

Handlers translate expected failures into stable domain codes and the existing HTTP envelope. At minimum:

- `CATALOG_NOT_FOUND` -> 404.
- `CATALOG_NAME_CONFLICT` -> 409.
- `REQUEST_CONFLICT` -> 409.
- `INVALID_PRICING_CONFIGURATION` -> 400.
- `INVALID_MODIFIER_CONFIGURATION` -> 400.
- `INVALID_INHERITANCE` -> 409.
- `ENTITY_RETIRED` -> 409.
- `INVALID_MANAGER_PIN` -> 403.
- `FORBIDDEN` -> 403.
- `UNAUTHORIZED` -> 401.
- `INVALID_STORED_RESULT` -> 500 with a generic client message and detailed server log.

Known unique, foreign-key, and check-constraint violations are translated inside the Catalog boundary. They must not become accidental generic 500 responses.

Validation messages use JSON field names. The shared validator should not report Go field names such as `baseprice` for `base_price`.

---

## 12. Testing

### 12.1 Unit Tests

- Name normalization and normalized keys.
- Direct-versus-sized pricing.
- Whole-VND bounds.
- Effective group inheritance, exclusion, direct assignment, and deduplication.
- Defaults and group bounds.
- Sellability.
- Availability and Retirement transitions.
- Projection filtering and price-field exclusion.
- Request fingerprint normalization and PIN exclusion.

### 12.2 PostgreSQL Integration Tests

- Migration and database constraints.
- Every successful command.
- Exact idempotent replay and conflicting key reuse.
- Truly concurrent duplicate execution.
- Concurrent normalized-name conflicts.
- Rollback on audit failure.
- Fresh-PIN verification before replay.
- Replay denial after role removal, identity disablement, or session revocation.
- Parent Retirement preventing child mutations.
- Atomic multi-table creation and assignment.
- Consistent multi-query snapshots.

### 12.3 HTTP Tests

- Authentication and route-to-capability mapping.
- UUID and request validation.
- Stable response envelopes, statuses, and error codes.
- PIN secrecy in logs, errors, responses, fingerprints, and audit details.
- Exact field separation among sellable, management, and availability projections.
- Current, unavailable, and retired visibility behavior.

### 12.4 Fixtures And Performance

Fixtures include direct-priced and sized items, required sugar/ice groups, optional priced toppings, category inheritance, direct assignments, exclusions, defaults, unavailable choices, and retired entities.

A benchmark assembles all three menu projections from a realistic cafe-sized dataset. Correctness, consistency, and information isolation take priority over an arbitrary SQL query count.

---

## 13. Acceptance Criteria

1. The disposable category package and API are replaced by `internal/catalog` with no compatibility layer.
2. All Catalog entities use UUIDs and whole-VND `BIGINT` prices.
3. Sizes use absolute prices.
4. All 22 TypeScript Catalog commands have equivalent Go REST commands.
5. Unsupported generic CRUD operations are absent.
6. Sellable, management, and availability projections enforce their capability and field-visibility contracts.
7. Every mutation is actor-scoped and idempotent; every successful state change is transactionally audited without duplicating events for replays or same-state no-ops.
8. Price-sensitive commands require current authority and a fresh Manager PIN, including replay attempts.
9. Availability and Retirement remain distinct lifecycle concepts.
10. Canonical rules correct the identified TypeScript edge cases around retired parents, unavailable defaults, exclusions, and retired required groups.
11. Expected database constraint failures map to stable API errors.
12. Unit, PostgreSQL integration, HTTP, concurrency, and projection-security tests pass.
13. Swagger documentation reflects the complete Catalog API.
14. Existing Auth behavior and non-Catalog test suites remain passing.
