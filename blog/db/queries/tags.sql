-- name: UpsertTag :one
INSERT INTO blog_tags (slug, name)
VALUES ($1, $2)
ON CONFLICT (slug) DO UPDATE SET name = blog_tags.name
RETURNING *;

-- name: GetTagBySlug :one
SELECT * FROM blog_tags WHERE slug = $1;

-- name: ListTagsWithPublished :many
SELECT t.*, count(p.id) AS post_count
FROM blog_tags t
JOIN blog_post_tags pt ON pt.tag_id = t.id
JOIN blog_posts p ON p.id = pt.post_id
WHERE p.status = 'published' AND p.deleted_at IS NULL
GROUP BY t.id
ORDER BY t.name;

-- name: ListTagsForPost :many
SELECT t.*
FROM blog_tags t
JOIN blog_post_tags pt ON pt.tag_id = t.id
WHERE pt.post_id = $1
ORDER BY t.name;

-- name: ClearPostTags :exec
DELETE FROM blog_post_tags WHERE post_id = $1;

-- name: AddPostTag :exec
INSERT INTO blog_post_tags (post_id, tag_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;
