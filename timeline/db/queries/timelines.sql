-- name: ListTimelines :many
SELECT * FROM timeline_timelines
ORDER BY name ASC;

-- name: ListPublicTimelines :many
SELECT * FROM timeline_timelines
WHERE is_public = true
ORDER BY name ASC;

-- name: GetTimeline :one
SELECT * FROM timeline_timelines
WHERE slug = @slug;

-- name: GetTimelineByID :one
SELECT * FROM timeline_timelines
WHERE id = @id;

-- name: CreateTimeline :one
INSERT INTO timeline_timelines (slug, name, description, start_date, end_date, is_public)
VALUES (@slug, @name, @description, @start_date, @end_date, @is_public)
RETURNING *;

-- name: UpdateTimeline :one
UPDATE timeline_timelines SET
    name = @name,
    description = @description,
    start_date = @start_date,
    end_date = @end_date,
    is_public = @is_public,
    updated_at = NOW()
WHERE slug = @slug
RETURNING *;

-- name: DeleteTimeline :exec
DELETE FROM timeline_timelines WHERE slug = @slug;
