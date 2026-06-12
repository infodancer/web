-- name: CreatePost :one
INSERT INTO blog_posts (slug, title, body_md, body_html, excerpt,
                        author_key, blog_id, status, external_id, published_at)
VALUES (@slug, @title, @body_md, @body_html, @excerpt,
        @author_key, @blog_id, @status, @external_id, @published_at)
RETURNING *;

-- name: UpdatePost :one
UPDATE blog_posts
SET slug = @slug,
    title = @title,
    body_md = @body_md,
    body_html = @body_html,
    excerpt = @excerpt,
    blog_id = @blog_id,
    updated_at = now()
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: SetPostStatus :one
UPDATE blog_posts
SET status = @status,
    published_at = CASE
        WHEN @status::text = 'published' THEN coalesce(published_at, now())
        ELSE published_at
    END,
    updated_at = now()
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeletePost :execrows
UPDATE blog_posts
SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetPostByID :one
SELECT sqlc.embed(p),
       a.slug AS author_slug, a.display_name AS author_name,
       coalesce(b.slug, '') AS blog_slug, coalesce(b.name, '') AS blog_name
FROM blog_posts p
JOIN blog_authors a ON a.user_key = p.author_key
LEFT JOIN blog_blogs b ON b.id = p.blog_id
WHERE p.id = $1 AND p.deleted_at IS NULL;

-- name: GetPostBySlug :one
SELECT sqlc.embed(p),
       a.slug AS author_slug, a.display_name AS author_name,
       coalesce(b.slug, '') AS blog_slug, coalesce(b.name, '') AS blog_name
FROM blog_posts p
JOIN blog_authors a ON a.user_key = p.author_key
LEFT JOIN blog_blogs b ON b.id = p.blog_id
WHERE p.slug = $1 AND p.deleted_at IS NULL;

-- name: ListPublishedPosts :many
SELECT sqlc.embed(p),
       a.slug AS author_slug, a.display_name AS author_name,
       coalesce(b.slug, '') AS blog_slug, coalesce(b.name, '') AS blog_name
FROM blog_posts p
JOIN blog_authors a ON a.user_key = p.author_key
LEFT JOIN blog_blogs b ON b.id = p.blog_id
WHERE p.status = 'published' AND p.deleted_at IS NULL
ORDER BY p.published_at DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountPublishedPosts :one
SELECT count(*) FROM blog_posts
WHERE status = 'published' AND deleted_at IS NULL;

-- name: ListPublishedPostsByBlog :many
SELECT sqlc.embed(p),
       a.slug AS author_slug, a.display_name AS author_name,
       coalesce(b.slug, '') AS blog_slug, coalesce(b.name, '') AS blog_name
FROM blog_posts p
JOIN blog_authors a ON a.user_key = p.author_key
JOIN blog_blogs b ON b.id = p.blog_id
WHERE b.slug = @blog_slug AND p.status = 'published' AND p.deleted_at IS NULL
ORDER BY p.published_at DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountPublishedPostsByBlog :one
SELECT count(*)
FROM blog_posts p
JOIN blog_blogs b ON b.id = p.blog_id
WHERE b.slug = $1 AND p.status = 'published' AND p.deleted_at IS NULL;

-- name: ListPublishedPostsByAuthor :many
SELECT sqlc.embed(p),
       a.slug AS author_slug, a.display_name AS author_name,
       coalesce(b.slug, '') AS blog_slug, coalesce(b.name, '') AS blog_name
FROM blog_posts p
JOIN blog_authors a ON a.user_key = p.author_key
LEFT JOIN blog_blogs b ON b.id = p.blog_id
WHERE a.slug = @author_slug AND p.status = 'published' AND p.deleted_at IS NULL
ORDER BY p.published_at DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountPublishedPostsByAuthor :one
SELECT count(*)
FROM blog_posts p
JOIN blog_authors a ON a.user_key = p.author_key
WHERE a.slug = $1 AND p.status = 'published' AND p.deleted_at IS NULL;

-- name: ListPublishedPostsByTag :many
SELECT sqlc.embed(p),
       a.slug AS author_slug, a.display_name AS author_name,
       coalesce(b.slug, '') AS blog_slug, coalesce(b.name, '') AS blog_name
FROM blog_posts p
JOIN blog_authors a ON a.user_key = p.author_key
LEFT JOIN blog_blogs b ON b.id = p.blog_id
JOIN blog_post_tags pt ON pt.post_id = p.id
JOIN blog_tags t ON t.id = pt.tag_id
WHERE t.slug = @tag_slug AND p.status = 'published' AND p.deleted_at IS NULL
ORDER BY p.published_at DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountPublishedPostsByTag :one
SELECT count(*)
FROM blog_posts p
JOIN blog_post_tags pt ON pt.post_id = p.id
JOIN blog_tags t ON t.id = pt.tag_id
WHERE t.slug = $1 AND p.status = 'published' AND p.deleted_at IS NULL;

-- name: ListPostsByAuthorKeyAnyStatus :many
SELECT sqlc.embed(p),
       a.slug AS author_slug, a.display_name AS author_name,
       coalesce(b.slug, '') AS blog_slug, coalesce(b.name, '') AS blog_name
FROM blog_posts p
JOIN blog_authors a ON a.user_key = p.author_key
LEFT JOIN blog_blogs b ON b.id = p.blog_id
WHERE p.author_key = @author_key AND p.deleted_at IS NULL
ORDER BY p.updated_at DESC
LIMIT @page_limit OFFSET @page_offset;
