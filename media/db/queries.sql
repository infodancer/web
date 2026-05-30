-- name: PutObject :exec
-- Insert object metadata. Idempotent: a re-upload of identical content
-- (same hash) is a no-op.
INSERT INTO media_objects (hash, content_type, width, height, byte_size, owner, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (hash) DO NOTHING;

-- name: GetObject :one
SELECT hash, content_type, width, height, byte_size, owner, created_at
FROM media_objects
WHERE hash = $1;

-- name: PutBlob :exec
-- Store bytes for a content address. Idempotent on the hash.
INSERT INTO media_blobs (hash, content_type, bytes, byte_size)
VALUES ($1, $2, $3, $4)
ON CONFLICT (hash) DO NOTHING;

-- name: GetBlob :one
SELECT bytes, content_type
FROM media_blobs
WHERE hash = $1;

-- name: HasBlob :one
SELECT EXISTS (SELECT 1 FROM media_blobs WHERE hash = $1);
