-- v0.3 schema: look up variants by their own content address.
--
-- Deleting an object removes a blob only when nothing else refers to it, and a
-- blob can be referenced as another original's variant (a small image's
-- thumbnail is often byte-identical to the image). That check queries
-- media_variants by (category, hash), which the existing parent index does not
-- cover.

-- +goose Up
CREATE INDEX media_variants_hash_idx
    ON media_variants (category, hash);

-- +goose Down
DROP INDEX media_variants_hash_idx;
