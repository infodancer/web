-- v0.2 schema: derived sizes (thumbnails). See DESIGN.md "Derived sizes".
--
-- A thumbnail is just another safe, content-addressed image: its bytes live in
-- the BlobStore under (category, hash) exactly like an original, so the serve
-- path needs no change. media_variants is only the map from an original to its
-- named variants — it does not store bytes.
--
-- Keyed (category, parent_hash, name): one variant per (original, spec name)
-- within a category. `hash` is the variant's own content address (its bytes),
-- distinct from parent_hash. Like every table here it is self-contained: no
-- foreign key into a host table, and `media_`-prefixed so it coexists with the
-- host schema.

-- +goose Up

CREATE TABLE media_variants (
    category     TEXT        NOT NULL,
    parent_hash  TEXT        NOT NULL,               -- original's content address
    name         TEXT        NOT NULL,               -- spec name, e.g. 'thumb'
    hash         TEXT        NOT NULL,               -- variant's own content address
    content_type TEXT        NOT NULL,
    width        INTEGER     NOT NULL,
    height       INTEGER     NOT NULL,
    byte_size    BIGINT      NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (category, parent_hash, name)
);

-- Listing every variant of one original.
CREATE INDEX media_variants_parent_idx
    ON media_variants (category, parent_hash);

-- +goose Down
DROP TABLE media_variants;
