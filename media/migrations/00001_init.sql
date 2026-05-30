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
-- media_objects. Identity (owner) is an opaque host-defined string, never a
-- foreign key into a host table, so the schema stays self-contained.

-- +goose Up

CREATE TABLE media_objects (
    hash         TEXT        PRIMARY KEY,            -- sha256 hex of canonical bytes
    content_type TEXT        NOT NULL,
    width        INTEGER     NOT NULL,
    height       INTEGER     NOT NULL,
    byte_size    BIGINT      NOT NULL,
    owner        TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Listing a given owner's uploads, newest first.
CREATE INDEX media_objects_owner_idx ON media_objects (owner, created_at DESC);

CREATE TABLE media_blobs (
    hash         TEXT        PRIMARY KEY,            -- same content address as media_objects
    content_type TEXT        NOT NULL,
    bytes        BYTEA       NOT NULL,
    byte_size    BIGINT      NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE media_blobs;
DROP TABLE media_objects;
