# Design Specification: Backend Alignment BA-1 — Catalog (`internal/catalog` + availability card cleanup)

- **Author:** Claude & Team
- **Date:** 2026-09-29
- **Status:** Draft, pending review
- **Epic:** [`docs/backlog/backend-alignment.md`](../../backlog/backend-alignment.md), sub-project BA-1
- **Supersedes:** [`docs/backlog/availability-card-fields.md`](../../backlog/availability-card-fields.md)
- **Predecessors:**
  - [`2026-09-10-catalog-slice-design.md`](2026-09-10-catalog-slice-design.md) (Phase 2 catalog)
  - [`2026-09-28-web-slice-9a-availability-design.md`](2026-09-28-web-slice-9a-availability-design.md) (splits slice 9, mocks card fields)
- **Visual authority:** [`design-system/pos-cafe/pages/settings.html`](../../../design-system/pos-cafe/pages/settings.html), tab "Quản lý Thực đơn & Topping" and its three modals plus the Batch Linker
- **Domain authority:** [`CONTEXT.md`](../../../CONTEXT.md), [`spec/decisions.md`](../../../spec/decisions.md) (ADR-048, ADR-055)

---

## 1. Purpose & Scope

Web slice 9b builds the catalog administration screens. The prototype's forms edit
fields and structure the Go catalog cannot store or change. BA-1 closes that gap on
the backend **before** 9b is designed, so 9b cuts nothing.

### 1.1 In Scope

- Display fields: Menu Item code, badge, description, image; Menu Category icon and
  display order (section 2).
- Item images stored as files and served on the LAN (section 3).
- New commands: item and category details, move item to category, add a Size, add a
  Modifier Option, change a group's selection rule, and three replace-set assignment
  commands (section 4).
- The three menu projections return the new fields; the availability projection
  returns prices to callers holding `catalog.view_prices` (section 5).
- The slice 9a availability cards read real values; `availability-mock.ts` is deleted
  (section 5.2).

### 1.2 Out of Scope

| Item | Why |
| --- | --- |
| Catalog administration UI (modals, Batch Linker) | Web slice 9b. |
| Reinstating a retired item, size, group, or option | Not in the prototype. YAGNI. |
| Converting an item between single price and sizes | Pricing mode stays fixed at creation (ADR-060). The 9b UI hides the toggle when editing. |
| Display order for items and modifier groups | Not in the prototype. |
| Server-side image transcoding or resizing | The browser resizes before upload; no image library enters the binary. |
| Deleting orphaned image files | Phase 12 ticket (section 7). |
| Standalone detach endpoints | Replace-set commands (4.3) express detach. |

---

## 2. Data Model

Migration `000016_add_catalog_display_fields.sql`.

### 2.1 `menu_items`

| Column | Type | Rule |
| --- | --- | --- |
| `code` | `TEXT NULL` | Display form as entered, after trim. |
| `normalized_code` | `TEXT NULL` | Lowercase; must match `^[a-z0-9]{1,12}$`. `NULL` exactly when `code` is `NULL` (check constraint). |
| `badge` | `TEXT NULL` | `CHECK (badge IN ('BEST_SELLER','HOT','NEW','SIGNATURE','CHEF_PICK'))`. |
| `description` | `TEXT NULL` | `CHECK (length(description) <= 300)`. Empty string is stored as `NULL`. |
| `image_key` | `TEXT NULL` | File name under `MEDIA_DIR/catalog/`, e.g. `<sha256>.webp`. |

Unique partial index:
`CREATE UNIQUE INDEX menu_items_active_code_key ON menu_items (normalized_code) WHERE retired_at IS NULL AND normalized_code IS NOT NULL;`
A retired item's code may be reused.

### 2.2 `menu_categories`

| Column | Type | Rule |
| --- | --- | --- |
| `icon` | `TEXT NULL` | `CHECK (icon ~ '^[a-z0-9-]{1,40}$')`. A Lucide icon name; the web owns the offered set. |
| `display_order` | `INT NOT NULL` | Added with default `0`, then backfilled by `row_number() OVER (ORDER BY created_at, id)`. No uniqueness; ties sort by name. |

### 2.3 Why these fields are not commercial facts

Code, badge, description, image, icon, and order change how the menu **looks**, never
what is sold or what it costs. They are edited in place, need no Manager Approval, and
are **not** copied into Committed Item snapshots (ADR-058). They still go through the
command pipeline: request identity, idempotency, and an Audit Event (ADR-048).

---

## 3. Item Images

### 3.1 Configuration

`config.Config` gains `MediaDir`, from env `MEDIA_DIR`, default `./data/media`.
`cmd/api/main.go` creates `MEDIA_DIR/catalog/` at startup (`os.MkdirAll`, `0o755`)
and fails fast if it cannot. `.env.example` documents the variable.

### 3.2 Upload: `PUT /api/v1/catalog/items/{item_id}/image`

- Capability `catalog.administer_structure`. No Manager PIN.
- `multipart/form-data` with fields `request_id` (UUID) and `file`.
- Request body capped at 1 MiB plus multipart overhead with `http.MaxBytesReader`; a
  larger file returns `413 IMAGE_TOO_LARGE`.
- The type is sniffed from content with `http.DetectContentType`, never taken from the
  header or file name. Accepted: `image/jpeg` → `.jpg`, `image/png` → `.png`,
  `image/webp` → `.webp`. Anything else returns `400 INVALID_IMAGE`.
- Key = `hex(sha256(bytes)) + ext`.
- **Write order:** the file is written to `catalog/.tmp-<random>` and renamed to its
  key *before* the database transaction begins. If the key already exists, the write
  is skipped (content addressing). The transaction then locks the item, rejects a
  retired item with `ENTITY_RETIRED`, sets `image_key`, and records the Audit Event.
  A rolled-back transaction leaves an unreferenced file, never a reference to a
  missing file.
- Idempotency: the fingerprint is `{item_id, sha256}`. An exact replay returns the
  stored result; the same `request_id` with different bytes returns `REQUEST_CONFLICT`.
- Audit: `catalog.item.image_set` with `{item_id, image_key, previous_image_key}`.
- Response: `ItemImageResponse {item_id, image_url}`.

### 3.3 Removal: `DELETE /api/v1/catalog/items/{item_id}/image`

JSON body `{request_id}`. Sets `image_key = NULL`. Audit `catalog.item.image_cleared`
with the previous key. The file stays on disk.

### 3.4 Serving: `GET /media/catalog/{key}`

- Mounted on the root Echo instance, **outside** `/api/v1` and outside `RequireAuth`.
  An `<img>` tag cannot send a bearer token, and a content hash cannot be guessed.
- `{key}` must match `^[0-9a-f]{64}\.(jpg|png|webp)$`; anything else is `404`. The
  path is joined only after the match, so traversal is impossible.
- Headers: `Cache-Control: public, max-age=31536000, immutable`, `Content-Type` from
  the extension, `X-Content-Type-Options: nosniff`.
- It must be registered before the SPA fallback, so `/media/...` never returns
  `index.html`.

Projections build `image_url = "/media/catalog/" + image_key`, relative, so the URL
works on any LAN host name. See ADR-057.

---

## 4. Commands

Every command follows the existing pipeline in `executor.go`: `request_id`,
idempotency through `catalog_mutation_requests`, row locks, authority reloaded inside
the transaction, and one Audit Event per command in the same transaction (ADR-048).
Existing endpoints are unchanged.

### 4.1 Display details

**1. `PATCH /catalog/items/{item_id}/details`** — capability `catalog.administer_structure`.

```json
{ "request_id": "…", "code": "cfsd" | null, "badge": "BEST_SELLER" | null, "description": "…" | null }
```

- All three fields are replaced; `null` clears. The body must carry all three keys
  (a missing key is `INVALID_INPUT`), so a stale client cannot partially overwrite.
- `code` is trimmed; if not `NULL` it must normalize to `^[a-z0-9]{1,12}$`, else
  `INVALID_INPUT`. A clash with another non-retired item returns
  `409 CATALOG_CODE_CONFLICT`.
- A retired item returns `ENTITY_RETIRED`.
- Audit `catalog.item.details_changed` with `before` and `after` of the three fields.

**2. `PATCH /catalog/categories/{category_id}/details`** — capability `catalog.administer_structure`.

```json
{ "request_id": "…", "icon": "coffee" | null, "display_order": 3 }
```

- `display_order` is `0` to `9999`. Other categories are not renumbered.
- A retired category returns `ENTITY_RETIRED`.
- Audit `catalog.category.details_changed` with `before` and `after`.

### 4.2 Structure

**3. `PATCH /catalog/items/{item_id}/category`** — capability `catalog.administer_structure`.

```json
{ "request_id": "…", "category_id": "…" }
```

- Locks the item, the source category, and the target category in id order.
- The target must exist and not be retired (`ENTITY_RETIRED`); the item must not be
  retired. Moving to the current category is a successful no-op.
- The item's name must be unique in the target (`UNIQUE (category_id, normalized_name)`),
  else `CATALOG_NAME_CONFLICT`.
- **Exclusion invariant (ADR-059):** exclusions of groups the target category does
  not provide are deleted in the same transaction.
- Audit `catalog.item.category_changed` with `{from_category_id, to_category_id, removed_exclusion_group_ids}`.

**4. `POST /catalog/items/{item_id}/sizes`** — capabilities `catalog.administer_structure` and `catalog.change_price`, fresh Manager PIN.

```json
{ "request_id": "…", "name": "Size XL", "price_vnd": 59000, "manager_pin": "…" }
```

- Only for a sized item (`price_vnd IS NULL`); a single-price item returns
  `INVALID_PRICING_CONFIGURATION` (ADR-060).
- Name non-empty and unique among the item's sizes, retired sizes included (the
  existing `UNIQUE (menu_item_id, normalized_name)`), else `CATALOG_NAME_CONFLICT`.
- Price validated by the existing `ValidatePrice`.
- New size is available. Audit `catalog.size.created`. Response `SizeResponse`.

**5. `POST /catalog/modifier-groups/{group_id}/options`** — capabilities `catalog.administer_structure` and `catalog.change_price`, fresh Manager PIN.

```json
{ "request_id": "…", "name": "Thạch dừa", "surcharge_vnd": 8000, "manager_pin": "…" }
```

- The group must not be retired. Name unique within the group, else
  `CATALOG_NAME_CONFLICT`. Surcharge `0` to `2147483647`.
- The new option is available and is not a default.
- Audit `catalog.modifier_option.created`. Response `ModifierOptionResponse`.

**6. `PUT /catalog/modifier-groups/{group_id}/selection-rule`** — capability `catalog.administer_structure`.

```json
{ "request_id": "…", "min_selections": 0, "max_selections": 3, "default_option_ids": ["…"] }
```

- Bounds and defaults change together so no intermediate state is ever invalid.
- Rules, each `INVALID_MODIFIER_CONFIGURATION`: `min ≥ 0`, `max ≥ 1`, `min ≤ max`,
  `max ≤` count of non-retired options, `min ≤ |defaults| ≤ max`, defaults unique,
  belong to the group, and are available and not retired.
- The group must not be retired.
- Audit `catalog.modifier_group.selection_rule_changed` with `before` and `after`.
- The existing `PUT /modifier-groups/{id}/defaults` stays for callers that change
  defaults alone.

### 4.3 Replace-set assignments

Each command replaces a whole set atomically, records one Audit Event listing
`added` and `removed`, and treats an identical set as a successful no-op. Detach is
expressed by omitting an id. Groups **added** by a command must not be retired;
already-attached retired groups may stay in the set.

**Exclusion invariant (ADR-059):** an exclusion row exists only while the item's
category provides that group. Commands 3, 8, and 9 delete exclusions that break it,
in the same transaction, and list them in the audit.

**7. `PUT /catalog/items/{item_id}/modifier-groups`** — capability `catalog.administer_structure`.

```json
{ "request_id": "…", "direct_group_ids": ["…"], "excluded_group_ids": ["…"] }
```

- `excluded_group_ids ⊆` groups the item's category provides, else `INVALID_INHERITANCE`.
- `direct_group_ids ∩ excluded_group_ids = ∅`, else `INVALID_INHERITANCE`.
- Duplicate ids in either list are `INVALID_INPUT`. Unknown ids are `CATALOG_NOT_FOUND`.
- The item must not be retired.
- Audit `catalog.item.modifier_groups_replaced` with `{direct: {added, removed}, excluded: {added, removed}}`.

**8. `PUT /catalog/categories/{category_id}/modifier-groups`** — capability `catalog.administer_structure`.

```json
{ "request_id": "…", "group_ids": ["…"] }
```

- For each removed group, exclusions of that group on items in this category are deleted.
- The category must not be retired.
- Audit `catalog.category.modifier_groups_replaced` with `{added, removed, removed_exclusions: [{item_id, group_id}]}`.

**9. `PUT /catalog/modifier-groups/{group_id}/assignments`** — capability `catalog.administer_structure`. Serves the 9b Batch Linker.

```json
{ "request_id": "…", "item_ids": ["…"], "category_ids": ["…"] }
```

- After the command, the group is attached **directly** to exactly `item_ids` and to
  exactly `category_ids`. Existing exclusions of this group are left as they are
  except where the invariant requires deletion.
- Adding an item that currently excludes this group is `INVALID_INHERITANCE`: the
  caller must lift the exclusion with command 7 first. This keeps "direct" and
  "excluded" from meaning both at once.
- Added items and categories must not be retired. Lists are capped at 500 ids each
  (`INVALID_INPUT` above).
- The group must not be retired.
- Locks: the group, then items and categories in id order.
- Audit `catalog.modifier_group.assignments_replaced` with `{items: {added, removed}, categories: {added, removed}, removed_exclusions}`.

### 4.4 Effect on Sales

None needed. Commit already re-reads the effective modifier groups and retirement at
commit time (`sql/queries/sales.sql`, effective groups query), so an Order Draft made
invalid by a detach or selection-rule change is rejected at commit exactly as a
retirement is today. Committed Items are immutable snapshots.

---

## 5. Projections

### 5.1 New fields

| Endpoint | Item | Category | Option |
| --- | --- | --- | --- |
| `GET /catalog/menu/manage` | `code`, `badge`, `description`, `image_url` | `icon`, `display_order` | — |
| `GET /catalog/menu/sellable` | `code`, `badge`, `image_url` | `icon` | — |
| `GET /catalog/menu/availability` | `code`, `image_url`, `price_vnd`* | `icon` | `surcharge_vnd`* |

\* Present only when the caller holds `catalog.view_prices` (`omitempty` pointer
fields). Barista does not hold it and receives neither. For a sized item `price_vnd`
is the lowest price among its non-retired sizes. Recorded in ADR-061.

- `code` is the **stored** code or `null`. The fallback to a name-derived acronym is
  a web rule (`getAcronym`), so the backend holds no Vietnamese word-splitting logic.
- `image_url` is `/media/catalog/<key>` or `null`.
- Every category list orders by `display_order`, then name.

### 5.2 Web cleanup in the same pull request

- `bun run codegen` against the regenerated `docs/swagger.yaml`.
- Delete `web/src/features/settings/lib/availability-mock.ts`.
- `availability-cards.ts` reads `image_url`, `price_vnd`, `surcharge_vnd`, and `code`
  (falling back to `getAcronym(name)` when `code` is `null`). A missing price shows "—".
- `web/src/lib/error-messages.ts` gains Vietnamese text for the three new codes.

No 9b screen is built here.

---

## 6. Errors

| Code | HTTP | Sentinel | When |
| --- | --- | --- | --- |
| `CATALOG_CODE_CONFLICT` | 409 | `ErrCodeConflict` | Code used by another non-retired item |
| `INVALID_IMAGE` | 400 | `ErrInvalidImage` | Sniffed type not JPEG, PNG, or WebP; empty file |
| `IMAGE_TOO_LARGE` | 413 | `ErrImageTooLarge` | Body over the cap |

The unique-index violation on `menu_items_active_code_key` is translated to
`ErrCodeConflict` in case of a race past the pre-check. All other failures reuse
existing codes.

---

## 7. Decisions and Documentation

New ADRs in `spec/decisions.md`:

- **ADR-057** — Catalog images are content-addressed files under `MEDIA_DIR`, served
  unauthenticated at `/media`. Rejected: bytes in PostgreSQL (heavier database and
  backups); external URLs (break without Internet, against Phase 11).
- **ADR-058** — Menu display fields are non-commercial: edited in place, no Manager
  Approval, not snapshotted. Code is optional with a client-derived fallback; badge is
  a closed enum.
- **ADR-059** — Modifier assignments have replace-set commands. An exclusion exists
  only while the item's category provides the group; detaching or moving deletes
  orphaned exclusions in the same transaction.
- **ADR-060** — Pricing mode (single price or Sizes) is fixed at item creation.
  Rejected: a conversion command, which needs rules for Order Drafts holding the item.
- **ADR-061** — The availability projection returns prices only to callers holding
  `catalog.view_prices`. Rejected: prices for everyone, which would hollow out the
  capability.

Other documents:

- `docs/swagger.yaml` regenerated with `make swagger`; SQL bindings with `make sqlc`.
- `docs/backlog/phase-12-backup-restore-update-readiness.md`: backup and restore must
  include `MEDIA_DIR`; unreferenced image files need a cleanup job.
- `docs/backlog/backend-alignment.md` and `ROADMAP.md`: BA-1 marked done after UAT.
- `docs/backlog/availability-card-fields.md`: closed.
- `scripts/dev-seed.ts`: seeds codes, badges, category icons, and display order. It
  seeds no images, so the seed runs offline.

---

## 8. Testing

Integration tests on the existing PostgreSQL template harness:

- **Each command:** success; every validation rule above; exact replay returns the
  stored result; same `request_id` with a different body returns `REQUEST_CONFLICT`;
  the Audit Event is written; a missing capability is rejected; a wrong Manager PIN
  is rejected for commands 4 and 5.
- **Exclusion invariant:** commands 3, 8, and 9 delete exactly the orphaned
  exclusions and list them in the audit.
- **Code uniqueness:** clash with an active item fails; reuse of a retired item's code
  succeeds.
- **Projections:** new fields present; category order follows `display_order`;
  availability omits prices for a caller without `catalog.view_prices`; lowest size
  price for sized items.
- **Images:** unit tests for type sniffing, size cap, and key regex; integration test
  that upload writes the file and sets `image_key`, and that replay does not rewrite;
  HTTP test for `/media` traversal rejection, cache headers, and not falling through
  to the SPA.
- **Web:** `bun test` for `availability-cards.ts` mapping without mocks.

---

## 9. UAT Gate

Performed by the operator through Swagger UI and the running web app:

1. Upload an image to an item; open its `image_url` in a new tab; the image renders.
2. Upload a `.txt` renamed to `.png`: `INVALID_IMAGE`. Upload a 2 MB file: `IMAGE_TOO_LARGE`.
3. Set code `cfsd` on two items: the second returns `CATALOG_CODE_CONFLICT`.
4. Set a category's icon and display order; `GET /catalog/menu/sellable` reflects the order.
5. Replace an item's modifier groups to drop "Mức đá"; the POS no longer offers it for that item.
6. Move an item to a category that lacks a group it excluded; the exclusion disappears from `/menu/manage`.
7. Add a Size to a sized item with Manager PIN; add one to a single-price item: rejected.
8. The "Món tạm hết" tab shows real prices, images, and codes as Manager; as Barista the price reads "—".

Implementation stops at this gate. Completion is not self-certified.
