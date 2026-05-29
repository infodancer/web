-- +goose Up
-- +goose StatementBegin

-- search_documents: one row per indexed document (DESIGN §3.1).
--
-- MVP scope: this migration creates the FTS-only corpus. The vector tables
-- (search_chunks + pgvector + the HNSW index, DESIGN §3.2) are deferred to
-- the embeddings milestone so the MVP runs on a vanilla Postgres with no
-- extension prerequisite. The columns reserved for v2 authorization
-- (visibility, authz_context) are present now so adding that path later is
-- additive, not a re-index (DESIGN §11.2).
CREATE TABLE search_documents (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source        text        NOT NULL,              -- 'hugo' | 'db'
    source_name   text        NOT NULL,              -- hugo: site key; db: view suffix
    source_key    text        NOT NULL,              -- hugo: public/-relative path; db: external_id
    url           text        NOT NULL,              -- canonical result URL
    title         text        NOT NULL DEFAULT '',
    section       text        NOT NULL DEFAULT '',   -- facet: top-level section
    lang          text        NOT NULL DEFAULT 'en', -- reserved for multilingual v2
    tags          text[]      NOT NULL DEFAULT '{}',
    headings      text        NOT NULL DEFAULT '',   -- concatenated heading text, weighted B
    summary       text        NOT NULL DEFAULT '',   -- frontmatter description / meta description
    snippet       text        NOT NULL DEFAULT '',   -- pre-escaped listing excerpt (FTS-headline fallback)
    body_text     text        NOT NULL DEFAULT '',   -- plain extracted text; FTS source; NEVER raw HTML
    content_hash  bytea       NOT NULL,              -- sha256 of normalized source; drives incremental indexing
    published_at  timestamptz,                       -- frontmatter date / source updated_at
    visibility    text        NOT NULL DEFAULT 'public'  -- reserved; v1 hard-refuses non-'public'
                  CHECK (visibility IN ('public','members','owners_only')),
    authz_context jsonb       NOT NULL DEFAULT '{}',     -- opaque; consumer-emitted; search NEVER interprets
    -- tags use array_to_tsvector (each tag becomes one verbatim lexeme) rather
    -- than the DESIGN §3.1 sketch's to_tsvector(array_to_string(...)): a stored
    -- generated column requires an IMMUTABLE expression, and array_to_string is
    -- only STABLE. array_to_tsvector is immutable and a better fit for tags —
    -- they match as exact lexemes, not stemmed words.
    tsv           tsvector GENERATED ALWAYS AS (
                      setweight(to_tsvector('english', coalesce(title,'')),     'A') ||
                      setweight(array_to_tsvector(tags),                        'B') ||
                      setweight(to_tsvector('english', coalesce(headings,'')),  'B') ||
                      setweight(to_tsvector('english', coalesce(body_text,'')), 'C')
                  ) STORED,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz,                       -- soft delete (reaping)
    UNIQUE (source, source_name, source_key)
);

CREATE INDEX search_documents_tsv      ON search_documents USING gin (tsv);
CREATE INDEX search_documents_live     ON search_documents (id) WHERE deleted_at IS NULL;
CREATE INDEX search_documents_section  ON search_documents (section)  WHERE deleted_at IS NULL;
CREATE INDEX search_documents_tags     ON search_documents USING gin (tags);
CREATE INDEX search_documents_reaped   ON search_documents (deleted_at) WHERE deleted_at IS NOT NULL;

-- search_index_meta: singleton recording index-wide invariants (DESIGN §3.3).
-- Read on every index run and on server open. embed_* stay NULL/0 until the
-- embeddings milestone wires go-embedding in.
CREATE TABLE search_index_meta (
    id            int PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    embed_model   text,
    embed_dim     int,
    embed_metric  text NOT NULL DEFAULT 'cosine',    -- 'cosine' | 'ip' | 'l2'
    fts_config    text NOT NULL DEFAULT 'english',
    base_url      text NOT NULL DEFAULT '',
    last_index_at timestamptz
);
INSERT INTO search_index_meta (id) VALUES (1) ON CONFLICT DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS search_index_meta;
DROP TABLE IF EXISTS search_documents;
-- +goose StatementEnd
