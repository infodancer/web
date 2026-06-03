-- name: ListCategories :many
SELECT * FROM categories
WHERE timeline_id = @timeline_id
ORDER BY sort_order ASC, name ASC;

-- name: GetCategory :one
SELECT * FROM categories
WHERE timeline_id = @timeline_id AND slug = @slug;

-- name: UpdateCategory :one
UPDATE categories SET
    name = @name,
    color = @color,
    sort_order = @sort_order
WHERE id = @id AND timeline_id = @timeline_id
RETURNING *;

-- name: DeleteCategory :exec
DELETE FROM categories WHERE id = @id AND timeline_id = @timeline_id;

-- name: CreateCategory :one
INSERT INTO categories (timeline_id, slug, name, color, sort_order)
VALUES (@timeline_id, @slug, @name, @color, @sort_order)
RETURNING *;
