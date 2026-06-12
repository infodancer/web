-- name: InsertAuthor :one
INSERT INTO blog_authors (user_key, slug, display_name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateAuthor :one
UPDATE blog_authors
SET slug = $2, display_name = $3, updated_at = now()
WHERE user_key = $1
RETURNING *;

-- name: GetAuthorByKey :one
SELECT * FROM blog_authors WHERE user_key = $1;

-- name: GetAuthorBySlug :one
SELECT * FROM blog_authors WHERE slug = $1;

-- name: ListAuthorsWithPublished :many
SELECT a.*, count(p.id) AS post_count
FROM blog_authors a
JOIN blog_posts p ON p.author_key = a.user_key
WHERE p.status = 'published' AND p.deleted_at IS NULL
GROUP BY a.user_key
ORDER BY a.display_name;
