-- SearchDocumentsFTS runs the full-text-search arm (DESIGN §6.1) and produces
-- a highlighted headline per row (DESIGN §6.5). User input is parsed with
-- websearch_to_tsquery, which has no FTS-syntax injection surface.
--
-- The headline wraps matched terms with the sentinel tokens ZZMARKSTARTZZ /
-- ZZMARKENDZZ rather than real markup. The query layer HTML-escapes the whole
-- headline and only then swaps the (now-escaped) sentinels for <mark> tags, so
-- the rendered snippet can never carry executable HTML from document text
-- (DESIGN §10: we never emit stored HTML).
--
-- name: SearchDocumentsFTS :many
SELECT
    d.id,
    d.url,
    d.title,
    d.section,
    d.tags,
    d.summary,
    d.snippet,
    d.body_text,
    ts_rank_cd(d.tsv, q) AS rank,
    ts_headline(
        'english',
        d.body_text,
        q,
        'StartSel=ZZMARKSTARTZZ, StopSel=ZZMARKENDZZ, MaxFragments=2, MinWords=5, MaxWords=22, FragmentDelimiter= … '
    ) AS headline
FROM search_documents d,
     websearch_to_tsquery('english', sqlc.arg(query)) q
WHERE d.tsv @@ q
  AND d.deleted_at IS NULL
ORDER BY rank DESC, d.id
LIMIT sqlc.arg(fanout);

-- CountLiveDocuments reports the number of indexed, non-soft-deleted documents.
-- Used by /healthz and the seed/serve diagnostics.
--
-- name: CountLiveDocuments :one
SELECT count(*) AS n
FROM search_documents
WHERE deleted_at IS NULL;

-- GetIndexMeta returns the singleton index-meta row (DESIGN §3.3), read on
-- server open and every index run.
--
-- name: GetIndexMeta :one
SELECT id, embed_model, embed_dim, embed_recipe, embed_metric, fts_config, base_url, last_index_at
FROM search_index_meta
WHERE id = 1;

-- GetDocumentsByIDs returns the display columns for a set of live documents,
-- preserving nothing about order (the caller re-orders). It hydrates documents
-- that surfaced only via the vector arm and so have no FTS row or headline
-- (DESIGN §6.2/§6.5); their snippet falls back to the stored excerpt.
--
-- name: GetDocumentsByIDs :many
SELECT id, url, title, section, tags, summary, snippet, body_text
FROM search_documents
WHERE id = ANY(sqlc.arg(ids)::bigint[])
  AND deleted_at IS NULL;

-- ── Ingestion write path (DESIGN §5) ──────────────────────────────────────────
-- These run only as the read-write indexer role. The serving query role has no
-- privilege to execute them (DESIGN §10).

-- GetDocumentHash returns the stored content hash for a live document's natural
-- key, or no rows when it was never indexed OR is currently soft-deleted. The
-- pipeline compares it to the incoming hash and skips the upsert (and any
-- re-embed) when they match — content_hash is the incremental-indexing cache
-- (DESIGN §5.1). Excluding soft-deleted rows means a reappearing document is
-- always re-upserted, which clears its deleted_at and revives it in place.
--
-- name: GetDocumentHash :one
SELECT content_hash
FROM search_documents
WHERE source = $1 AND source_name = $2 AND source_key = $3
  AND deleted_at IS NULL;

-- UpsertDocument inserts or updates one document by its natural key, clearing any
-- prior soft-delete so a reappearing document is revived in place. tsv is a
-- generated column and is recomputed automatically. Returns the row id so the
-- caller can attach chunks (embeddings milestone).
--
-- name: UpsertDocument :one
INSERT INTO search_documents (
    source, source_name, source_key, url, title, section, lang, tags,
    headings, summary, snippet, body_text, content_hash, published_at,
    visibility, authz_context
) VALUES (
    sqlc.arg(source), sqlc.arg(source_name), sqlc.arg(source_key), sqlc.arg(url),
    sqlc.arg(title), sqlc.arg(section), sqlc.arg(lang), sqlc.arg(tags),
    sqlc.arg(headings), sqlc.arg(summary), sqlc.arg(snippet), sqlc.arg(body_text),
    sqlc.arg(content_hash), sqlc.arg(published_at), sqlc.arg(visibility),
    sqlc.arg(authz_context)
)
ON CONFLICT (source, source_name, source_key) DO UPDATE SET
    url           = EXCLUDED.url,
    title         = EXCLUDED.title,
    section       = EXCLUDED.section,
    lang          = EXCLUDED.lang,
    tags          = EXCLUDED.tags,
    headings      = EXCLUDED.headings,
    summary       = EXCLUDED.summary,
    snippet       = EXCLUDED.snippet,
    body_text     = EXCLUDED.body_text,
    content_hash  = EXCLUDED.content_hash,
    published_at  = EXCLUDED.published_at,
    visibility    = EXCLUDED.visibility,
    authz_context = EXCLUDED.authz_context,
    deleted_at    = NULL,
    updated_at    = now()
RETURNING id;

-- SoftDeleteDocument marks one document deleted by natural key (DESIGN §5.4).
-- It is a no-op (0 rows) when the document is missing or already deleted, which
-- lets the Delete API report whether anything was actually removed.
--
-- name: SoftDeleteDocument :execrows
UPDATE search_documents
SET deleted_at = now(), updated_at = now()
WHERE source = $1 AND source_name = $2 AND source_key = $3
  AND deleted_at IS NULL;

-- ListLiveSourceKeys returns the natural keys of every live document in a source.
-- Reconcile diffs this against the keys it was handed to find the absent set.
--
-- name: ListLiveSourceKeys :many
SELECT source_key
FROM search_documents
WHERE source = $1 AND source_name = $2 AND deleted_at IS NULL;

-- SoftDeleteSourceKeys soft-deletes the named keys within a source in one
-- statement. Reconcile passes the absent set after the bulk-reap guard has
-- approved the count (DESIGN §5.4).
--
-- name: SoftDeleteSourceKeys :execrows
UPDATE search_documents
SET deleted_at = now(), updated_at = now()
WHERE source = $1 AND source_name = $2
  AND deleted_at IS NULL
  AND source_key = ANY(sqlc.arg(source_keys)::text[]);

-- ReapExpired hard-deletes documents soft-deleted before the cutoff, reclaiming
-- their rows (and, once embeddings land, their chunks via ON DELETE CASCADE).
-- The cutoff is now() - retention, computed by the caller.
--
-- name: ReapExpired :execrows
DELETE FROM search_documents
WHERE deleted_at IS NOT NULL AND deleted_at < $1;
