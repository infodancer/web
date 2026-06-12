-- +goose Up

-- Named blogs: distinct editorial streams within one blog instance (e.g.
-- "Admin Notes", a weekly column). They share the author pool and tag
-- namespace; each gets its own index page and feeds.
CREATE TABLE blog_blogs (
    id          BIGSERIAL PRIMARY KEY,
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Author registry: a display-identity cache for users who have authored
-- posts, NOT an identity table. Identity and roles live with the host
-- (resolver + authz); display data is refreshed on save from the host's
-- profile component when wired. user_key is the host's opaque stable id.
CREATE TABLE blog_authors (
    user_key     TEXT PRIMARY KEY,
    slug         TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE blog_posts (
    id           BIGSERIAL PRIMARY KEY,
    slug         TEXT NOT NULL UNIQUE,
    title        TEXT NOT NULL,
    body_md      TEXT NOT NULL,
    -- Sanitized render cache, regenerated on every save by the single
    -- audited markdown pipeline. Reads never re-render.
    body_html    TEXT NOT NULL,
    excerpt      TEXT NOT NULL DEFAULT '',
    author_key   TEXT NOT NULL REFERENCES blog_authors(user_key),
    blog_id      BIGINT REFERENCES blog_blogs(id) ON DELETE SET NULL,
    status       TEXT NOT NULL CHECK (status IN ('draft', 'published', 'archived')),
    -- Opaque host-defined linkage; blog stores and indexes it, never
    -- interprets it.
    external_id  TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Set on first publish, preserved across unpublish/republish so the
    -- original publication date survives editing.
    published_at TIMESTAMPTZ,
    deleted_at   TIMESTAMPTZ
);

CREATE INDEX blog_posts_pub_idx ON blog_posts (status, published_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX blog_posts_author_idx ON blog_posts (author_key)
    WHERE deleted_at IS NULL;
CREATE INDEX blog_posts_blog_idx ON blog_posts (blog_id)
    WHERE blog_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX blog_posts_external_idx ON blog_posts (external_id)
    WHERE external_id IS NOT NULL;

CREATE TABLE blog_tags (
    id   BIGSERIAL PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

CREATE TABLE blog_post_tags (
    post_id BIGINT NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
    tag_id  BIGINT NOT NULL REFERENCES blog_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (post_id, tag_id)
);
CREATE INDEX blog_post_tags_tag_idx ON blog_post_tags (tag_id);

-- +goose Down
DROP TABLE blog_post_tags;
DROP TABLE blog_tags;
DROP TABLE blog_posts;
DROP TABLE blog_authors;
DROP TABLE blog_blogs;
