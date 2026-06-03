-- name: ListCategories :many
SELECT * FROM timeline_categories
WHERE timeline_id = @timeline_id
ORDER BY sort_order ASC, name ASC;

-- name: GetCategory :one
SELECT * FROM timeline_categories
WHERE timeline_id = @timeline_id AND slug = @slug;

-- name: UpdateCategory :one
UPDATE timeline_categories SET
    name = @name,
    color = @color,
    sort_order = @sort_order
WHERE id = @id AND timeline_id = @timeline_id
RETURNING *;

-- name: DeleteCategory :exec
DELETE FROM timeline_categories WHERE id = @id AND timeline_id = @timeline_id;

-- name: CreateCategory :one
INSERT INTO timeline_categories (timeline_id, slug, name, color, sort_order)
VALUES (@timeline_id, @slug, @name, @color, @sort_order)
RETURNING *;
