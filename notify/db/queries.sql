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

-- AdminFeed returns events newest-first, with optional filters and keyset
-- pagination. By default it returns only admin-audience events (the curated
-- admin stream); pass all_audiences = true for the full activity firehose
-- across every audience. A NULL filter argument means "no constraint"; pass a
-- non-zero before_id to fetch the page strictly older than that id.
-- name: AdminFeed :many
SELECT
    id, source, type, title, severity, audience,
    actor_issuer, actor_subject, actor_label,
    subject_issuer, subject_subject, payload, created_at
FROM notify_events
WHERE (sqlc.arg('all_audiences')::bool OR audience = 'admin')
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

-- InsertUserNotification fans one event out to one recipient. Idempotent: a
-- redundant Emit (same event, same recipient) is a no-op, so the fan-out can be
-- retried without double-delivering.
-- name: InsertUserNotification :exec
INSERT INTO notify_user_notifications (event_id, recipient_issuer, recipient_subject)
VALUES (sqlc.arg('event_id'), $1, $2)
ON CONFLICT (event_id, recipient_issuer, recipient_subject) DO NOTHING;

-- UserFeed returns one recipient's notifications newest-first, joined to the
-- underlying event, excluding dismissed rows. Keyset pagination is over the
-- event id (a recipient holds at most one row per event), matching the admin
-- feed's cursor shape.
-- name: UserFeed :many
SELECT
    e.id, e.source, e.type, e.title, e.severity, e.audience,
    e.actor_issuer, e.actor_subject, e.actor_label,
    e.subject_issuer, e.subject_subject, e.payload, e.created_at,
    n.read_at, n.dismissed_at
FROM notify_user_notifications n
JOIN notify_events e ON e.id = n.event_id
WHERE n.recipient_issuer = $1 AND n.recipient_subject = $2
  AND n.dismissed_at IS NULL
  AND (sqlc.narg('before_id')::bigint IS NULL OR e.id < sqlc.narg('before_id'))
ORDER BY e.created_at DESC, e.id DESC
LIMIT sqlc.arg('lim');

-- UserUnreadCount counts a recipient's notifications that are neither read nor
-- dismissed — the number for an unread badge.
-- name: UserUnreadCount :one
SELECT count(*)
FROM notify_user_notifications
WHERE recipient_issuer = $1 AND recipient_subject = $2
  AND read_at IS NULL AND dismissed_at IS NULL;

-- MarkUserRead stamps read_at for one recipient's view of one event. COALESCE
-- keeps the first read instant, so re-marking is a no-op.
-- name: MarkUserRead :exec
UPDATE notify_user_notifications
SET read_at = COALESCE(read_at, now())
WHERE recipient_issuer = $1 AND recipient_subject = $2 AND event_id = sqlc.arg('event_id');

-- DismissUser stamps dismissed_at for one recipient's view of one event,
-- removing it from the feed and the unread count. COALESCE keeps the first
-- dismissal instant.
-- name: DismissUser :exec
UPDATE notify_user_notifications
SET dismissed_at = COALESCE(dismissed_at, now())
WHERE recipient_issuer = $1 AND recipient_subject = $2 AND event_id = sqlc.arg('event_id');
