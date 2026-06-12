-- name: CreateBlog :one
INSERT INTO blog_blogs (slug, name, description)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateBlog :one
UPDATE blog_blogs
SET slug = $2, name = $3, description = $4
WHERE id = $1
RETURNING *;

-- name: DeleteBlog :execrows
DELETE FROM blog_blogs WHERE id = $1;

-- name: GetBlogByID :one
SELECT * FROM blog_blogs WHERE id = $1;

-- name: GetBlogBySlug :one
SELECT * FROM blog_blogs WHERE slug = $1;

-- name: ListBlogs :many
SELECT b.*,
       (SELECT count(*) FROM blog_posts p
         WHERE p.blog_id = b.id AND p.deleted_at IS NULL) AS post_count
FROM blog_blogs b
ORDER BY b.name;

-- name: CountPostsReferencingBlog :one
SELECT count(*) FROM blog_posts
WHERE blog_id = $1 AND deleted_at IS NULL;
