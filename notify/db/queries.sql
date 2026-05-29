-- InsertEvent appends one event to the append-only log. created_at is set by
-- the database so ordering is consistent across producers.
-- name: InsertEvent :one
INSERT INTO notify_events (
    source, type, title, severity, audience,
    actor_issuer, actor_subject, actor_label,
    subject_issuer, subject_subject, payload
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8,
    $9, $10, $11
)
RETURNING id, created_at;

-- AdminFeed returns admin-audience events newest-first, with optional filters
-- and keyset pagination. A NULL filter argument means "no constraint"; pass a
-- non-zero before_id to fetch the page strictly older than that id.
-- name: AdminFeed :many
SELECT
    id, source, type, title, severity, audience,
    actor_issuer, actor_subject, actor_label,
    subject_issuer, subject_subject, payload, created_at
FROM notify_events
WHERE audience = 'admin'
  AND (sqlc.narg('source')::text   IS NULL OR source   = sqlc.narg('source'))
  AND (sqlc.narg('type')::text     IS NULL OR type     = sqlc.narg('type'))
  AND (sqlc.narg('severity')::text IS NULL OR severity = sqlc.narg('severity'))
  AND (sqlc.narg('since')::timestamptz IS NULL OR created_at >= sqlc.narg('since'))
  AND (sqlc.narg('until')::timestamptz IS NULL OR created_at <= sqlc.narg('until'))
  AND (sqlc.narg('before_id')::bigint  IS NULL OR id < sqlc.narg('before_id'))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('lim');

-- AdminUnreadCount counts admin events newer than the given cursor instant.
-- name: AdminUnreadCount :one
SELECT count(*)
FROM notify_events
WHERE audience = 'admin'
  AND created_at > sqlc.arg('cursor');

-- AdminCursor returns the admin's last_seen_at, or no rows if never seen.
-- name: AdminCursor :one
SELECT last_seen_at
FROM notify_admin_read_cursor
WHERE issuer = $1 AND subject = $2;

-- AdminMarkSeen advances the admin's cursor monotonically; it never moves
-- backwards, so a stale request cannot un-see newer events.
-- name: AdminMarkSeen :exec
INSERT INTO notify_admin_read_cursor (issuer, subject, last_seen_at, updated_at)
VALUES ($1, $2, sqlc.arg('up_to'), now())
ON CONFLICT (issuer, subject) DO UPDATE
SET last_seen_at = greatest(notify_admin_read_cursor.last_seen_at, excluded.last_seen_at),
    updated_at   = now();
