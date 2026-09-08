-- name: CreateCategory :one
INSERT INTO categories (
    name,
    description,
    display_order,
    is_active,
    created_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
)
RETURNING *;

-- name: GetCategoryByID :one
SELECT * FROM categories
WHERE id = $1 LIMIT 1;

-- name: GetCategoryByName :one
SELECT * FROM categories
WHERE name = $1 LIMIT 1;

-- name: ListCategories :many
SELECT * FROM categories
ORDER BY display_order ASC, name ASC;

-- name: ListActiveCategories :many
SELECT * FROM categories
WHERE is_active = TRUE
ORDER BY display_order ASC, name ASC;

-- name: UpdateCategory :one
UPDATE categories
SET
    name = $1,
    description = $2,
    display_order = $3,
    is_active = $4,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $5
RETURNING *;

-- name: DeleteCategory :exec
DELETE FROM categories
WHERE id = $1;
