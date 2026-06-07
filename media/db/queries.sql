-- name: PutObject :exec
-- Insert object metadata. Idempotent: a re-upload of identical content
-- (same category + hash) is a no-op.
INSERT INTO media_objects (category, hash, content_type, width, height, byte_size, subject, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (category, hash) DO NOTHING;

-- name: GetObject :one
SELECT category, hash, content_type, width, height, byte_size, subject, created_at
FROM media_objects
WHERE category = $1 AND hash = $2;

-- name: ListObjectsBySubject :many
-- All images tied to one owning record within a category, newest first.
SELECT category, hash, content_type, width, height, byte_size, subject, created_at
FROM media_objects
WHERE category = $1 AND subject = $2
ORDER BY created_at DESC, hash;

-- name: ListObjectsAfter :many
-- A page of objects in a category, keyset-paginated by the primary key (hash)
-- so a backfill can walk the whole category. Pass the empty string as the
-- cursor to start (it sorts before any hex hash); pass the last hash of a page
-- to fetch the next. Index-backed by the (category, hash) primary key.
SELECT category, hash, content_type, width, height, byte_size, subject, created_at
FROM media_objects
WHERE category = $1 AND hash > $2
ORDER BY hash
LIMIT $3;

-- name: PutBlob :exec
-- Store bytes for a content address. Idempotent on (category, hash).
INSERT INTO media_blobs (category, hash, content_type, bytes, byte_size)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (category, hash) DO NOTHING;

-- name: GetBlob :one
SELECT bytes, content_type
FROM media_blobs
WHERE category = $1 AND hash = $2;

-- name: HasBlob :one
SELECT EXISTS (SELECT 1 FROM media_blobs WHERE category = $1 AND hash = $2);

-- name: PutVariant :exec
-- Map an original to one named derived size. Idempotent on
-- (category, parent_hash, name). The variant's bytes are stored separately in
-- the BlobStore under (category, hash).
INSERT INTO media_variants (category, parent_hash, name, hash, content_type, width, height, byte_size, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (category, parent_hash, name) DO NOTHING;

-- name: ListVariants :many
-- Every derived size of one original, by spec name.
SELECT category, parent_hash, name, hash, content_type, width, height, byte_size, created_at
FROM media_variants
WHERE category = $1 AND parent_hash = $2
ORDER BY name;
