-- Catalog sqlc queries
-- Authorization, advisory-lock, idempotency, audit, and entity CRUD primitives.

-- name: GetCatalogSessionAuthority :one
SELECT s.id AS session_id, s.staff_identity_id, s.state, s.active_workspace,
       s.last_human_activity_at, s.expires_at, s.revoked_at,
       i.enabled AS identity_enabled, i.pin_hash
FROM staff_access_sessions s
JOIN staff_identities i ON i.id = s.staff_identity_id
WHERE s.id = $1 AND s.staff_identity_id = $2;

-- name: GetCatalogSessionRoles :many
SELECT role
FROM staff_operational_roles
WHERE staff_identity_id = $1
ORDER BY role ASC;

-- name: ClaimCatalogRequest :one
INSERT INTO catalog_mutation_requests
    (actor_id, request_id, operation, request_hash, response_code, response_body)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (actor_id, request_id) DO UPDATE
    SET response_code = catalog_mutation_requests.response_code,
        response_body = catalog_mutation_requests.response_body
RETURNING actor_id, request_id, operation, request_hash,
          response_code, response_body, created_at;

-- name: GetCatalogMutationRequest :one
SELECT actor_id, request_id, operation, request_hash,
       response_code, response_body, created_at
FROM catalog_mutation_requests
WHERE actor_id = $1 AND request_id = $2;

-- name: StoreCatalogRequestResult :exec
UPDATE catalog_mutation_requests
SET response_code = $3, response_body = $4
WHERE actor_id = $1 AND request_id = $2;

-- name: InsertAuditEvent :one
INSERT INTO audit_events (event_type, actor_id, session_id, details, occurred_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, event_type, actor_id, session_id, details, occurred_at;

-- name: ListAuditEvents :many
SELECT id, event_type, actor_id, session_id, details, occurred_at
FROM audit_events
WHERE event_type LIKE $1 || '%'
ORDER BY occurred_at DESC, id DESC
LIMIT $2;

-- -- Menu Categories --

-- name: CreateMenuCategory :one
INSERT INTO menu_categories (name, normalized_name)
VALUES ($1, $2)
RETURNING id, name, normalized_name, created_at;

-- name: GetMenuCategoryByID :one
SELECT id, name, normalized_name, created_at
FROM menu_categories
WHERE id = $1;

-- name: GetMenuCategoryForUpdate :one
SELECT id, name, normalized_name, created_at
FROM menu_categories
WHERE id = $1
FOR UPDATE;

-- name: RenameMenuCategory :one
UPDATE menu_categories
SET name = $2, normalized_name = $3
WHERE id = $1
RETURNING id, name, normalized_name, created_at;

-- name: ListMenuCategories :many
SELECT id, name, normalized_name, created_at
FROM menu_categories
ORDER BY normalized_name ASC, id ASC;

-- -- Menu Items --

-- name: CreateMenuItem :one
INSERT INTO menu_items
    (category_id, name, normalized_name, price_vnd, available)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, category_id, name, normalized_name, price_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: GetMenuItemByID :one
SELECT id, category_id, name, normalized_name, price_vnd,
       available, retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM menu_items
WHERE id = $1;

-- name: GetMenuItemForUpdate :one
SELECT id, category_id, name, normalized_name, price_vnd,
       available, retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM menu_items
WHERE id = $1
FOR UPDATE;

-- name: RenameMenuItem :one
UPDATE menu_items
SET name = $2, normalized_name = $3, updated_at = now()
WHERE id = $1
RETURNING id, category_id, name, normalized_name, price_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: RepriceMenuItem :one
UPDATE menu_items
SET price_vnd = $2, updated_at = now()
WHERE id = $1
RETURNING id, category_id, name, normalized_name, price_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: SetMenuItemAvailability :one
UPDATE menu_items
SET available = $2, updated_at = now()
WHERE id = $1
RETURNING id, category_id, name, normalized_name, price_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: RetireMenuItem :one
UPDATE menu_items
SET retired_at = $2, retirement_reason = $3, retirement_note = $4, updated_at = now()
WHERE id = $1
RETURNING id, category_id, name, normalized_name, price_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: ListMenuItemsByCategory :many
SELECT id, category_id, name, normalized_name, price_vnd,
       available, retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM menu_items
WHERE category_id = $1
ORDER BY normalized_name ASC, id ASC;

-- -- Menu Item Sizes --

-- name: CreateMenuItemSize :one
INSERT INTO menu_item_sizes
    (menu_item_id, name, normalized_name, price_vnd, available)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, menu_item_id, name, normalized_name, price_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: GetMenuItemSizeByID :one
SELECT id, menu_item_id, name, normalized_name, price_vnd,
       available, retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM menu_item_sizes
WHERE id = $1;

-- name: GetMenuItemSizeForUpdate :one
SELECT id, menu_item_id, name, normalized_name, price_vnd,
       available, retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM menu_item_sizes
WHERE id = $1
FOR UPDATE;

-- name: RenameMenuItemSize :one
UPDATE menu_item_sizes
SET name = $2, normalized_name = $3, updated_at = now()
WHERE id = $1
RETURNING id, menu_item_id, name, normalized_name, price_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: RepriceMenuItemSize :one
UPDATE menu_item_sizes
SET price_vnd = $2, updated_at = now()
WHERE id = $1
RETURNING id, menu_item_id, name, normalized_name, price_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: SetMenuItemSizeAvailability :one
UPDATE menu_item_sizes
SET available = $2, updated_at = now()
WHERE id = $1
RETURNING id, menu_item_id, name, normalized_name, price_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: RetireMenuItemSize :one
UPDATE menu_item_sizes
SET retired_at = $2, retirement_reason = $3, retirement_note = $4, updated_at = now()
WHERE id = $1
RETURNING id, menu_item_id, name, normalized_name, price_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: ListMenuItemSizesByItem :many
SELECT id, menu_item_id, name, normalized_name, price_vnd,
       available, retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM menu_item_sizes
WHERE menu_item_id = $1
ORDER BY normalized_name ASC, id ASC;

-- -- Modifier Groups --

-- name: CreateModifierGroup :one
INSERT INTO modifier_groups (name, normalized_name, min_selections, max_selections)
VALUES ($1, $2, $3, $4)
RETURNING id, name, normalized_name, min_selections, max_selections,
          retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: GetModifierGroupByID :one
SELECT id, name, normalized_name, min_selections, max_selections,
       retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM modifier_groups
WHERE id = $1;

-- name: GetModifierGroupForUpdate :one
SELECT id, name, normalized_name, min_selections, max_selections,
       retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM modifier_groups
WHERE id = $1
FOR UPDATE;

-- name: RenameModifierGroup :one
UPDATE modifier_groups
SET name = $2, normalized_name = $3, updated_at = now()
WHERE id = $1
RETURNING id, name, normalized_name, min_selections, max_selections,
          retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: RetireModifierGroup :one
UPDATE modifier_groups
SET retired_at = $2, retirement_reason = $3, retirement_note = $4, updated_at = now()
WHERE id = $1
RETURNING id, name, normalized_name, min_selections, max_selections,
          retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: ListModifierGroups :many
SELECT id, name, normalized_name, min_selections, max_selections,
       retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM modifier_groups
ORDER BY normalized_name ASC, id ASC;

-- -- Modifier Options --

-- name: CreateModifierOption :one
INSERT INTO modifier_options
    (modifier_group_id, name, normalized_name, surcharge_vnd, available)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, modifier_group_id, name, normalized_name, surcharge_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: GetModifierOptionByID :one
SELECT id, modifier_group_id, name, normalized_name, surcharge_vnd,
       available, retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM modifier_options
WHERE id = $1;

-- name: GetModifierOptionForUpdate :one
SELECT id, modifier_group_id, name, normalized_name, surcharge_vnd,
       available, retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM modifier_options
WHERE id = $1
FOR UPDATE;

-- name: RenameModifierOption :one
UPDATE modifier_options
SET name = $2, normalized_name = $3, updated_at = now()
WHERE id = $1
RETURNING id, modifier_group_id, name, normalized_name, surcharge_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: RepriceModifierOption :one
UPDATE modifier_options
SET surcharge_vnd = $2, updated_at = now()
WHERE id = $1
RETURNING id, modifier_group_id, name, normalized_name, surcharge_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: SetModifierOptionAvailability :one
UPDATE modifier_options
SET available = $2, updated_at = now()
WHERE id = $1
RETURNING id, modifier_group_id, name, normalized_name, surcharge_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: RetireModifierOption :one
UPDATE modifier_options
SET retired_at = $2, retirement_reason = $3, retirement_note = $4, updated_at = now()
WHERE id = $1
RETURNING id, modifier_group_id, name, normalized_name, surcharge_vnd,
          available, retired_at, retirement_reason, retirement_note,
          created_at, updated_at;

-- name: ListModifierOptionsByGroup :many
SELECT id, modifier_group_id, name, normalized_name, surcharge_vnd,
       available, retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM modifier_options
WHERE modifier_group_id = $1
ORDER BY normalized_name ASC, id ASC;

-- -- Association Queries --

-- name: CreateItemModifierGroup :exec
INSERT INTO item_modifier_groups (menu_item_id, modifier_group_id)
VALUES ($1, $2);

-- name: CreateCategoryModifierGroup :exec
INSERT INTO category_modifier_groups (menu_category_id, modifier_group_id)
VALUES ($1, $2);

-- name: CreateItemModifierGroupExclusion :exec
INSERT INTO item_modifier_group_exclusions (menu_item_id, modifier_group_id)
VALUES ($1, $2);

-- name: CreateModifierGroupDefaultOption :exec
INSERT INTO modifier_group_default_options (modifier_group_id, modifier_option_id)
VALUES ($1, $2);

-- name: DeleteModifierGroupDefaultOptions :exec
DELETE FROM modifier_group_default_options
WHERE modifier_group_id = $1;

-- name: GetCategoryModifierGroup :one
SELECT menu_category_id, modifier_group_id, created_at
FROM category_modifier_groups
WHERE menu_category_id = $1 AND modifier_group_id = $2;

-- name: ListItemModifierGroupsByItem :many
SELECT imgr.modifier_group_id, mg.name, mg.normalized_name,
       mg.min_selections, mg.max_selections,
       mg.retired_at, mg.retirement_reason, mg.retirement_note
FROM item_modifier_groups imgr
JOIN modifier_groups mg ON mg.id = imgr.modifier_group_id
WHERE imgr.menu_item_id = $1
ORDER BY mg.normalized_name ASC, mg.id ASC;

-- name: ListCategoryModifierGroupsByCategory :many
SELECT cmgr.modifier_group_id, mg.name, mg.normalized_name,
       mg.min_selections, mg.max_selections,
       mg.retired_at, mg.retirement_reason, mg.retirement_note
FROM category_modifier_groups cmgr
JOIN modifier_groups mg ON mg.id = cmgr.modifier_group_id
WHERE cmgr.menu_category_id = $1
ORDER BY mg.normalized_name ASC, mg.id ASC;

-- name: ListItemModifierGroupExclusionsByItem :many
SELECT ime.modifier_group_id, mg.name, mg.normalized_name
FROM item_modifier_group_exclusions ime
JOIN modifier_groups mg ON mg.id = ime.modifier_group_id
WHERE ime.menu_item_id = $1
ORDER BY mg.normalized_name ASC, mg.id ASC;

-- name: ListModifierGroupDefaultOptionsByGroup :many
SELECT mgdo.modifier_option_id, mo.name, mo.normalized_name,
       mo.surcharge_vnd, mo.available,
       mo.retired_at, mo.retirement_reason, mo.retirement_note
FROM modifier_group_default_options mgdo
JOIN modifier_options mo ON mo.id = mgdo.modifier_option_id
WHERE mgdo.modifier_group_id = $1
ORDER BY mo.normalized_name ASC, mo.id ASC;

-- -- Advisory Lock --

-- name: CatalogAdvisoryLock :exec
SELECT pg_advisory_xact_lock($1);

-- -- Paginated Reads --

-- name: ListMenuItemsByCategoryPaginated :many
SELECT id, category_id, name, normalized_name, price_vnd,
       available, retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM menu_items
WHERE category_id = $1
ORDER BY normalized_name ASC, id ASC
LIMIT $2 OFFSET $3;

-- name: ListAllMenuItemsPaginated :many
SELECT id, category_id, name, normalized_name, price_vnd,
       available, retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM menu_items
ORDER BY normalized_name ASC, id ASC
LIMIT $1 OFFSET $2;

-- name: ListAllMenuItemSizesPaginated :many
SELECT id, menu_item_id, name, normalized_name, price_vnd,
       available, retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM menu_item_sizes
ORDER BY normalized_name ASC, id ASC
LIMIT $1 OFFSET $2;

-- name: ListAllModifierGroupsPaginated :many
SELECT id, name, normalized_name, min_selections, max_selections,
       retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM modifier_groups
ORDER BY normalized_name ASC, id ASC
LIMIT $1 OFFSET $2;

-- name: ListAllModifierOptionsPaginated :many
SELECT id, modifier_group_id, name, normalized_name, surcharge_vnd,
       available, retired_at, retirement_reason, retirement_note,
       created_at, updated_at
FROM modifier_options
ORDER BY normalized_name ASC, id ASC
LIMIT $1 OFFSET $2;
