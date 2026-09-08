-- name: CreateCategory :one
INSERT INTO categories (
    name,
    description,
    display_order,
    is_active,
    created_at,
    updated_at
) VALUES (
    ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
)
RETURNING *;

-- name: GetCategoryByID :one
SELECT * FROM categories
WHERE id = ? LIMIT 1;

-- name: GetCategoryByName :one
SELECT * FROM categories
WHERE name = ? LIMIT 1;

-- name: ListCategories :many
SELECT * FROM categories
ORDER BY display_order ASC, name ASC;

-- name: ListActiveCategories :many
SELECT * FROM categories
WHERE is_active = 1
ORDER BY display_order ASC, name ASC;

-- name: UpdateCategory :one
UPDATE categories
SET
    name = ?,
    description = ?,
    display_order = ?,
    is_active = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: DeleteCategory :exec
DELETE FROM categories
WHERE id = ?;
