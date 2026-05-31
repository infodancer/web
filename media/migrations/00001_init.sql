-- v0.1 schema for the media module. See DESIGN.md for rationale.
--
-- Convention: every table is prefixed `media_` so the module's schema can
-- coexist with the host application's schema in a shared database, and the
-- goose history lives in its own version table (see migrate.go).
--
-- Two tables, mirroring the BlobStore seam: media_objects holds queryable
-- metadata; media_blobs holds the bytes. The default PostgresBlobStore owns
-- media_blobs and is self-contained (it carries its own content_type), so a
-- future filesystem/object-store backend can replace it without touching
-- media_objects.
--
-- Both tables are keyed by (category, hash). `category` is the server-side
-- bucket a handler stores under (e.g. 'campaign', 'creature', 'character') —
-- never client-supplied — giving physical separation alongside the hash
-- content address. `subject` ties an image to the owning record it belongs to
-- (e.g. a campaign id); like all identity here it is an opaque host-defined
-- string, never a foreign key into a host table, so the schema stays
-- self-contained.

-- +goose Up

CREATE TABLE media_objects (
    category     TEXT        NOT NULL,
    hash         TEXT        NOT NULL,               -- sha256 hex of canonical bytes
    content_type TEXT        NOT NULL,
    width        INTEGER     NOT NULL,
    height       INTEGER     NOT NULL,
    byte_size    BIGINT      NOT NULL,
    subject      TEXT        NOT NULL DEFAULT '',    -- owning record id (Config.Authorize)
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (category, hash)
);

-- Listing the images tied to a given record within a category, newest first.
CREATE INDEX media_objects_subject_idx
    ON media_objects (category, subject, created_at DESC);

CREATE TABLE media_blobs (
    category     TEXT        NOT NULL,
    hash         TEXT        NOT NULL,               -- same content address as media_objects
    content_type TEXT        NOT NULL,
    bytes        BYTEA       NOT NULL,
    byte_size    BIGINT      NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (category, hash)
);

-- +goose Down
DROP TABLE media_blobs;
DROP TABLE media_objects;
