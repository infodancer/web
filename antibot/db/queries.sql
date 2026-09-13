-- name: Hit :one
-- Count one submission against a window, atomically. Concurrent submissions
-- on the same key serialize on the primary key, so none can read a stale count
-- and slip past the limit.
INSERT INTO antibot_counters (form, key_kind, key_hash, window_start, hits, expires_at)
VALUES (sqlc.arg(form), sqlc.arg(key_kind), sqlc.arg(key_hash), sqlc.arg(window_start), 1, sqlc.arg(expires_at))
ON CONFLICT (form, key_kind, key_hash, window_start)
DO UPDATE SET hits = antibot_counters.hits + 1
RETURNING hits;

-- name: Sweep :execrows
DELETE FROM antibot_counters WHERE expires_at < sqlc.arg(now);
