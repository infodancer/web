-- name: ListEventSources :many
SELECT * FROM timeline_event_sources
WHERE event_id = @event_id
ORDER BY sort_order ASC;

-- name: GetEventSource :one
SELECT * FROM timeline_event_sources WHERE id = @id;

-- name: UpdateEventSource :one
UPDATE timeline_event_sources SET
    url = @url,
    title = @title,
    author = @author,
    citation = @citation,
    sort_order = @sort_order
WHERE id = @id
RETURNING *;

-- name: DeleteEventSource :exec
DELETE FROM timeline_event_sources WHERE id = @id;

-- name: SearchEventSourcesByURL :many
SELECT * FROM timeline_event_sources
WHERE event_id IN (SELECT id FROM timeline_events WHERE timeline_id = @timeline_id)
  AND url ILIKE '%' || @query || '%'
ORDER BY url ASC
LIMIT @max_results;

-- name: AddEventSource :one
INSERT INTO timeline_event_sources (event_id, url, title, author, citation, sort_order)
VALUES (@event_id, @url, @title, @author, @citation, @sort_order)
RETURNING *;
