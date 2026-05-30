-- name: InsertSubscriber :execrows
INSERT INTO subscribers (email) VALUES ($1)
ON CONFLICT DO NOTHING;

-- name: MarkSynced :exec
UPDATE subscribers SET synced_at = NOW() WHERE email = $1;
