-- name: ListTags :many
SELECT t.*, COUNT(et.event_id) AS event_count
FROM tags t
LEFT JOIN event_tags et ON et.tag_id = t.id
LEFT JOIN events e ON e.id = et.event_id AND e.timeline_id = @timeline_id
GROUP BY t.id
HAVING COUNT(et.event_id) > 0
ORDER BY t.name ASC;

-- name: GetOrCreateTag :one
INSERT INTO tags (slug, name)
VALUES (@slug, @name)
ON CONFLICT (slug) DO UPDATE SET slug = EXCLUDED.slug
RETURNING *;

-- name: AddEventTag :exec
INSERT INTO event_tags (event_id, tag_id)
VALUES (@event_id, @tag_id)
ON CONFLICT DO NOTHING;

-- name: RemoveEventTag :exec
DELETE FROM event_tags WHERE event_id = @event_id AND tag_id = @tag_id;

-- name: ListEventTags :many
SELECT t.*
FROM tags t
JOIN event_tags et ON et.tag_id = t.id
WHERE et.event_id = @event_id
ORDER BY t.name ASC;
