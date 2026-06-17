-- name: UpsertNotice :exec
-- Record one occurrence of a failure. The first occurrence inserts the row;
-- repeats (same fingerprint) bump count and advance last_seen, refreshing the
-- last-occurrence fields (request_id, status, method, path, panic_value, stack)
-- while leaving first_seen untouched. A crash loop is therefore one row with a
-- rising count, not thousands of rows.
INSERT INTO error_notices (
    fingerprint, request_id, component, method, path, status,
    panic_value, stack, first_seen, last_seen, count
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9, 1)
ON CONFLICT (fingerprint) DO UPDATE SET
    last_seen   = excluded.last_seen,
    count       = error_notices.count + 1,
    request_id  = excluded.request_id,
    status      = excluded.status,
    method      = excluded.method,
    path        = excluded.path,
    panic_value = excluded.panic_value,
    stack       = excluded.stack;

-- name: ListRecent :many
-- The admin "recent errors" view: the most recently active failures first,
-- index-backed by error_notices_last_seen_idx.
SELECT id, fingerprint, request_id, component, method, path, status,
       panic_value, stack, first_seen, last_seen, count
FROM error_notices
ORDER BY last_seen DESC, id DESC
LIMIT $1;
