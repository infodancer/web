-- name: UpsertTimeline :one
INSERT INTO timeline_timelines (slug, name, description, is_public)
VALUES (@slug, @name, @description, @is_public)
ON CONFLICT (slug) DO UPDATE SET
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    is_public = EXCLUDED.is_public,
    updated_at = NOW()
RETURNING *;

-- name: UpsertCategory :one
INSERT INTO timeline_categories (timeline_id, slug, name, color, sort_order)
VALUES (@timeline_id, @slug, @name, @color, @sort_order)
ON CONFLICT (timeline_id, slug) DO UPDATE SET
    name = EXCLUDED.name,
    color = EXCLUDED.color,
    sort_order = EXCLUDED.sort_order
RETURNING *;

-- name: UpsertPerson :one
INSERT INTO timeline_people (timeline_id, slug, name, sort_name, bio, born_date, died_date, photo_url)
VALUES (@timeline_id, @slug, @name, @sort_name, @bio, @born_date, @died_date, @photo_url)
ON CONFLICT (timeline_id, slug) DO UPDATE SET
    name = EXCLUDED.name,
    sort_name = EXCLUDED.sort_name,
    bio = EXCLUDED.bio,
    born_date = EXCLUDED.born_date,
    died_date = EXCLUDED.died_date,
    photo_url = EXCLUDED.photo_url,
    updated_at = NOW()
RETURNING *;

-- name: UpsertEvent :one
INSERT INTO timeline_events (timeline_id, slug, title, summary, article, event_date, event_end_date, date_precision, sort_order, category_id)
VALUES (@timeline_id, @slug, @title, @summary, @article, @event_date, @event_end_date, @date_precision, @sort_order, @category_id)
ON CONFLICT (timeline_id, slug) DO UPDATE SET
    title = EXCLUDED.title,
    summary = EXCLUDED.summary,
    article = EXCLUDED.article,
    event_date = EXCLUDED.event_date,
    event_end_date = EXCLUDED.event_end_date,
    date_precision = EXCLUDED.date_precision,
    sort_order = EXCLUDED.sort_order,
    category_id = EXCLUDED.category_id,
    updated_at = NOW()
RETURNING *;
