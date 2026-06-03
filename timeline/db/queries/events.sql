-- name: ListEventsByDateRange :many
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color
FROM events e
LEFT JOIN categories c ON c.id = e.category_id
WHERE e.timeline_id = @timeline_id
  AND e.event_date >= @from_date
  AND e.event_date <= @to_date
ORDER BY e.event_date ASC, e.sort_order ASC
LIMIT @max_results;

-- name: ListEventsByDateRangeAndCategory :many
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color
FROM events e
LEFT JOIN categories c ON c.id = e.category_id
WHERE e.timeline_id = @timeline_id
  AND e.category_id = @category_id
  AND e.event_date >= @from_date
  AND e.event_date <= @to_date
ORDER BY e.event_date ASC, e.sort_order ASC
LIMIT @max_results;

-- name: ListEventsAfterDate :many
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color
FROM events e
LEFT JOIN categories c ON c.id = e.category_id
WHERE e.timeline_id = @timeline_id
  AND (e.event_date > @after_date OR (e.event_date = @after_date AND e.sort_order > @after_sort_order))
ORDER BY e.event_date ASC, e.sort_order ASC
LIMIT @max_results;

-- name: ListEventsBeforeDate :many
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color
FROM events e
LEFT JOIN categories c ON c.id = e.category_id
WHERE e.timeline_id = @timeline_id
  AND (e.event_date < @before_date OR (e.event_date = @before_date AND e.sort_order < @before_sort_order))
ORDER BY e.event_date DESC, e.sort_order DESC
LIMIT @max_results;

-- name: ListEventsAfterDateByCategory :many
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color
FROM events e
LEFT JOIN categories c ON c.id = e.category_id
WHERE e.timeline_id = @timeline_id
  AND e.category_id = @category_id
  AND (e.event_date > @after_date OR (e.event_date = @after_date AND e.sort_order > @after_sort_order))
ORDER BY e.event_date ASC, e.sort_order ASC
LIMIT @max_results;

-- name: ListEventsBeforeDateByCategory :many
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color
FROM events e
LEFT JOIN categories c ON c.id = e.category_id
WHERE e.timeline_id = @timeline_id
  AND e.category_id = @category_id
  AND (e.event_date < @before_date OR (e.event_date = @before_date AND e.sort_order < @before_sort_order))
ORDER BY e.event_date DESC, e.sort_order DESC
LIMIT @max_results;

-- name: ListEventsAfterDateByTag :many
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color
FROM events e
LEFT JOIN categories c ON c.id = e.category_id
JOIN event_tags et ON et.event_id = e.id
WHERE e.timeline_id = @timeline_id
  AND et.tag_id = @tag_id
  AND (e.event_date > @after_date OR (e.event_date = @after_date AND e.sort_order > @after_sort_order))
ORDER BY e.event_date ASC, e.sort_order ASC
LIMIT @max_results;

-- name: ListEventsBeforeDateByTag :many
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color
FROM events e
LEFT JOIN categories c ON c.id = e.category_id
JOIN event_tags et ON et.event_id = e.id
WHERE e.timeline_id = @timeline_id
  AND et.tag_id = @tag_id
  AND (e.event_date < @before_date OR (e.event_date = @before_date AND e.sort_order < @before_sort_order))
ORDER BY e.event_date DESC, e.sort_order DESC
LIMIT @max_results;

-- name: GetEvent :one
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color
FROM events e
LEFT JOIN categories c ON c.id = e.category_id
WHERE e.timeline_id = @timeline_id
  AND e.slug = @slug;

-- name: GetEventByID :one
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color
FROM events e
LEFT JOIN categories c ON c.id = e.category_id
WHERE e.id = @id;

-- name: CountEventsByTimeline :one
SELECT COUNT(*) FROM events
WHERE timeline_id = @timeline_id;

-- name: GetTimelineDateBounds :one
SELECT MIN(event_date) AS min_date, MAX(event_date) AS max_date
FROM events
WHERE timeline_id = @timeline_id;

-- name: CreateEvent :one
INSERT INTO events (timeline_id, slug, title, summary, article, event_date, event_end_date, date_precision, sort_order, category_id)
VALUES (@timeline_id, @slug, @title, @summary, @article, @event_date, @event_end_date, @date_precision, @sort_order, @category_id)
RETURNING *;

-- name: UpdateEvent :one
UPDATE events SET
    title = @title,
    summary = @summary,
    article = @article,
    event_date = @event_date,
    event_end_date = @event_end_date,
    date_precision = @date_precision,
    sort_order = @sort_order,
    category_id = @category_id,
    updated_at = NOW()
WHERE id = @id AND timeline_id = @timeline_id
RETURNING *;

-- name: DeleteEvent :exec
DELETE FROM events WHERE id = @id AND timeline_id = @timeline_id;

-- name: GetEventBySlug :one
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color
FROM events e
LEFT JOIN categories c ON c.id = e.category_id
WHERE e.timeline_id = @timeline_id
  AND e.slug = @slug;

-- name: SearchEvents :many
SELECT e.*,
       c.name AS category_name,
       c.color AS category_color,
       ts_rank(to_tsvector('english', e.title || ' ' || e.summary || ' ' || e.article), plainto_tsquery('english', @query)) AS rank
FROM events e
LEFT JOIN categories c ON c.id = e.category_id
WHERE e.timeline_id = @timeline_id
  AND to_tsvector('english', e.title || ' ' || e.summary || ' ' || e.article) @@ plainto_tsquery('english', @query)
ORDER BY rank DESC
LIMIT @max_results;
