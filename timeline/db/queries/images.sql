-- name: UpsertImage :one
INSERT INTO images (timeline_id, slug, usage, content_type, width, height, file_size)
VALUES (@timeline_id, @slug, @usage, @content_type, @width, @height, @file_size)
ON CONFLICT (timeline_id, slug) DO UPDATE SET
    usage = EXCLUDED.usage,
    content_type = EXCLUDED.content_type,
    width = EXCLUDED.width,
    height = EXCLUDED.height,
    file_size = EXCLUDED.file_size
RETURNING *;

-- name: GetImage :one
SELECT * FROM images WHERE id = @id;

-- name: GetImageBySlug :one
SELECT * FROM images WHERE timeline_id = @timeline_id AND slug = @slug;

-- name: ListImagesByTimeline :many
SELECT * FROM images WHERE timeline_id = @timeline_id ORDER BY slug;

-- name: ListImagesByUsage :many
SELECT * FROM images WHERE timeline_id = @timeline_id AND usage = @usage ORDER BY slug;

-- name: DeleteImage :exec
DELETE FROM images WHERE id = @id;
