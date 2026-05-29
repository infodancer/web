# infodancer/search — Design Specification

**Module:** `github.com/infodancer/web/search`
**License:** Apache-2.0
**Visibility:** Private (consumed by tagged version + `go mod vendor`, like the rest of the suite)
**Status:** Design — guides implementation. No code yet.
**Go:** 1.25+

This document is the authoritative design for the search module. It is written to
guide a test-driven implementation. Where it says *reserved* or *v2*, the v1 code
must leave the door open exactly as described so the later change is additive, not a
re-index or a breaking migration.

---

## 1. Purpose

A reusable search tool for the InfoDancer / matthewjhunter web properties. It runs
**after a Hugo site is built**, reads the generated content, and serves search
results through an HTMX UI that consumes the `infodancer/ui` chrome.

Two content origins:

1. **Hugo content** — the built `public/` HTML plus the markdown source's
   frontmatter.
2. **Database-backed content** — relational content owned by a consuming
   application (e.g. `infodancer/blog` posts, OSG session notes), mostly Markdown,
   exposed to the indexer through a SQL view contract.

Search defaults to Postgres full-text search. When an embedding model is supplied,
it additionally stores chunk embeddings and fuses vector search with FTS.

### 1.1 Goals

- One importable Go module + one CLI binary; minimal mount surface for host apps.
- Postgres is the single backing store (FTS + `pgvector`).
- Canonical, correct result URLs derived from Hugo's own output.
- Decoupled DB-content integration: consumers describe their content without search
  knowing their schema and without linking their Go into the indexer.
- Forward-compatible with a complex per-identity authorization model (OSG) without
  building it in v1.
- Visibly secure by design: parameterized queries, injection-safe FTS, no raw
  third-party HTML served, least-privilege DB roles, same-origin CSRF guard.

### 1.2 Non-goals (v1)

- Per-user / authenticated result filtering (public-only; see §11).
- Multilingual indexing (single language; schema reserves `lang`).
- A WYSIWYG or in-page content preview pane (results are a ranked link list).
- Crawling remote sites over HTTP. The indexer reads the local filesystem and the
  database only.
- Distributed / multi-node indexing. The indexer is a single-process batch job.

---

## 2. Architecture

One repository, one binary (`cmd/search`) with two subcommands, plus the importable
`search` package.

```
search index   — post-build batch job. Reads public/ HTML + markdown frontmatter and
                 registered SQL views; parses, chunks, embeds, upserts; reconciles
                 deletions. Connects to Postgres as the READ-WRITE indexer role.

search serve   — long-running standalone HTTP server. Connects as the READ-ONLY query
                 role. Equivalent to mounting the library in a bare host.

import "github.com/infodancer/web/search"
        m, _ := search.New(cfg); mux.Handle("/search/", http.StripPrefix("/search", m.Routes()))
                 — mount inside a host app (osg, blog, faq host). The host supplies
                 its own pool (its choice of role).
```

The split is deliberate: indexing and serving have different lifecycles and
different trust. The serving surface has no write path to the index tables, so a
query-layer bug cannot corrupt the corpus.

### 2.1 Repository layout

```
search/
  go.mod                      module github.com/infodancer/web/search
  DESIGN.md                   this file
  README.md  SECURITY.md  CONTRIBUTING.md
  search.go                   package search: Module, New, Routes, Migrate
  config.go                   search.Config (serving) + applyDefaults
  source.go                   Source registration, ContentSource interface, Document
  user.go                     reserved v2: Viewer, VisibilityFilter, VisibilityState
  cmd/search/                 main; index + serve subcommands; TOML config + flags
  internal/index/             crawl, parse, section, chunk, embed, upsert, reconcile
  internal/hugo/              config reader; public/ path -> canonical URL
  internal/extract/           HTML (goquery) and Markdown sectioners -> []Section
  internal/query/             FTS arm, vector arm, RRF fusion, snippets, pagination
  internal/handler/           HTMX handlers, CSRF, static
  store/                      sqlc-generated queries, pgxpool wiring (store.go)
  migrations/                 goose .sql migrations
  templates/                  embedded html/template (overlays infodancer/ui)
  static/                     embedded css/js (htmx vendored, pinned + SRI)
```

Conventions mirror `faq`: `pgxpool` + `goose` + `sqlc`, embedded `templates`/`static`
with optional host overlays, `infodancer/ui` chrome with a standalone fallback,
`log/slog` structured logging, CI = `go test -race` + `golangci-lint` + `govulncheck`.

---

## 3. Data model

All tables are owned by the RW indexer role. The RO query role gets `SELECT` only.
`pgvector` is an operator prerequisite (`CREATE EXTENSION vector;`), documented in
README; the RW role need not own the extension.

### 3.1 `search_documents`

One row per indexed document (a Hugo page or a DB-source record).

```sql
CREATE TABLE search_documents (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source        text        NOT NULL,              -- 'hugo' | 'db'
    source_name   text        NOT NULL,              -- hugo: site key (often ''); db: view suffix e.g. 'blog'
    source_key    text        NOT NULL,              -- hugo: public/-relative path; db: external_id
    url           text        NOT NULL,              -- canonical result URL
    title         text        NOT NULL DEFAULT '',
    section       text        NOT NULL DEFAULT '',   -- facet: top-level hugo section / consumer-defined
    lang          text        NOT NULL DEFAULT 'en', -- reserved for multilingual v2
    tags          text[]      NOT NULL DEFAULT '{}',
    headings      text        NOT NULL DEFAULT '',   -- concatenated heading text, weighted B
    summary       text        NOT NULL DEFAULT '',   -- frontmatter description / meta description
    snippet       text        NOT NULL DEFAULT '',   -- pre-escaped listing excerpt (fallback when no FTS headline)
    body_text     text        NOT NULL DEFAULT '',   -- plain extracted text; FTS source; NEVER raw HTML
    content_hash  bytea       NOT NULL,              -- sha256 of normalized source; drives incremental indexing
    published_at  timestamptz,                       -- frontmatter date / source updated_at
    visibility    text        NOT NULL DEFAULT 'public'  -- reserved; v1 hard-refuses non-'public'
                  CHECK (visibility IN ('public','members','owners_only')),
    authz_context jsonb       NOT NULL DEFAULT '{}',     -- opaque; consumer-emitted; search NEVER interprets
    tsv           tsvector GENERATED ALWAYS AS (
                      setweight(to_tsvector('english', coalesce(title,'')),                    'A') ||
                      setweight(to_tsvector('english', coalesce(array_to_string(tags,' '),'')),'B') ||
                      setweight(to_tsvector('english', coalesce(headings,'')),                 'B') ||
                      setweight(to_tsvector('english', coalesce(body_text,'')),                'C')
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
```

The FTS configuration (`'english'`) is fixed in the generated-column expression at
migration time, set from Hugo's `defaultContentLanguage`. This is why multilingual is
deferred: a per-row FTS config cannot be a single generated column. v2 multilingual
replaces this with a per-row config + computed `tsvector` and is an explicit migration.

### 3.2 `search_chunks`

One row per heading-delimited chunk. Holds the vector embeddings.

```sql
CREATE TABLE search_chunks (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id    bigint NOT NULL REFERENCES search_documents(id) ON DELETE CASCADE,
    ord            int    NOT NULL,                  -- chunk order within the document
    heading_path   text   NOT NULL DEFAULT '',       -- breadcrumb, e.g. "Combat > Initiative"
    text           text   NOT NULL,                  -- chunk plain text (pre-format embed input)
    token_estimate int    NOT NULL DEFAULT 0,
    embedding      vector,                            -- dim set at first index (see §5.3); NULL until embedded
    UNIQUE (document_id, ord)
);
```

The entire table — `CREATE EXTENSION vector`, the table itself with
`embedding vector(<dim>)`, and the HNSW index — is created during the embedding
bootstrap (§5.3), **not in a static migration**, because the dimension is unknown
until the first `Embed` call. Creating it lazily also means an **FTS-only deployment
never needs the pgvector extension**: nothing in the goose migrations references it.
The bootstrap is serialized across processes by a Postgres advisory lock so concurrent
first writes cannot race the DDL.

```sql
-- run by the embedding bootstrap once embed_dim is known (ops class chosen from
-- embed_metric: cosine -> vector_cosine_ops, ip -> vector_ip_ops, l2 -> vector_l2_ops):
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE IF NOT EXISTS search_chunks ( ... embedding vector(<dim>) NOT NULL ... );
CREATE INDEX search_chunks_embedding ON search_chunks
    USING hnsw (embedding <ops_class_for_metric>);
-- a later model/dimension change (gated like --reembed) drops the chunks and
-- ALTERs the column type to vector(<newdim>) before rebuilding the index.
```

The module hardcodes no embedding metric: `embed_metric` is operator config, defaults
to `cosine`, and is recorded in `search_index_meta`. Cosine is the model-agnostic
default — it scores by angle and is invariant to vector magnitude, so it behaves well
for both normalized and unnormalized models; `ip`/`l2` are available for models that
recommend them. Changing the metric is treated like a fingerprint-era change: it
forces an index rebuild (drop + recreate the HNSW index under the new ops class),
gated the same way as `--reembed`.

Vector search joins chunks to live documents and filters `deleted_at IS NULL`; a
soft-deleted document's chunks are therefore excluded without touching the chunk rows.

### 3.3 `search_index_meta`

Singleton row recording index-wide invariants. Read on every run and on server open.

```sql
CREATE TABLE search_index_meta (
    id            int PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    embed_model   text,                              -- embedding.Fingerprint.Model
    embed_dim     int,                               -- embedding.Fingerprint.Dim (0 until first embed)
    embed_metric  text NOT NULL DEFAULT 'cosine',    -- 'cosine' | 'ip' | 'l2'; selects the HNSW ops class
    fts_config    text NOT NULL DEFAULT 'english',
    base_url      text NOT NULL DEFAULT '',          -- Hugo baseURL used for URL derivation
    last_index_at timestamptz
);
INSERT INTO search_index_meta (id) VALUES (1) ON CONFLICT DO NOTHING;
```

---

## 4. Content sources

### 4.1 Hugo ingestion

**Source of truth: the built `public/` HTML**, enriched with markdown frontmatter.
The HTML is what is actually published (shortcodes and render hooks applied), and its
location in `public/` *is* the canonical URL.

**Config reader (`internal/hugo`).** Reads only the keys the module needs, honoring
Hugo's config layering (`hugo.toml`/`config.toml`/`config/_default/`, environment
directories, and the active environment). Keys consumed:

- `baseURL` — *required*; the base for canonical URLs and stored in `index_meta`.
- `title` — default `SiteName` for the serving chrome.
- `defaultContentLanguage` — selects the Postgres FTS config (mapping table:
  `en→english`, `fr→french`, …; unknown → `simple` with a warning).
- `languages` — read but unused in v1 (single language).

This is a narrow reader, not a Viper clone. Unknown/extra config is ignored.

**Canonical URL derivation.** Derived from the `public/`-relative path against
`baseURL`, *not* by reimplementing Hugo's permalink engine — Hugo already resolved
`uglyURLs`/`canonifyURLs`/permalink rules when it wrote the file.

```
public/notes/foo/index.html  ->  {baseURL}/notes/foo/
public/about.html            ->  {baseURL}/about.html      (ugly URLs)
public/index.html            ->  {baseURL}/
```

Files excluded from indexing: anything not `*.html`; Hugo's generated non-page
outputs (`sitemap.xml`, `index.xml`, `404.html`, `index.json` and other custom output
formats); pages whose extracted content node is empty.

**Content extraction (`internal/extract`).** Parse the HTML with `goquery`, select the
main content node by a configurable CSS selector (default candidate list tried in
order: `main`, `article`, `.content`, falling back to `body` with nav/header/footer/
aside stripped). From that node produce:

- `title` — frontmatter `title`, else `<h1>`, else `<title>`.
- `headings` — concatenated `h1..h6` text (weighted B in FTS).
- `body_text` — the node's text content, whitespace-normalized. **Plain text only;
  raw page HTML is never stored or served**, which removes the result-rendering
  injection surface entirely.
- `[]Section` — the heading-delimited sections for chunking (§5.2).

**Frontmatter.** The markdown source is located from the `public/` path (reverse of
Hugo's content→output mapping; fall back to matching by `source_key` when ambiguous)
and parsed for `title`, `date` (→ `published_at`), `tags`, `description` (→ `summary`),
and `draft`. **Draft pages are not indexed.** Frontmatter wins over HTML-derived
metadata where both exist.

### 4.2 Database-backed content — the SQL view contract

A consuming application exposes each body of content as a SQL **view** named
`search_source_<name>`. The indexer is configured with the list of source names and
reads each view over its DB connection alone — **no consumer Go is linked into the
indexer.** The consumer owns its schema; search owns indexing; they couple only on
the column contract below.

```sql
-- Example: infodancer/blog exposes its published posts.
CREATE VIEW search_source_blog AS
SELECT
    p.id::text                         AS external_id,    -- stable id within the source
    '/blog/' || p.slug                 AS url,            -- canonical (absolute or root-relative)
    p.title                            AS title,
    p.body_md                          AS body,           -- content in the body_kind format
    'markdown'                         AS body_kind,       -- 'markdown' | 'html' | 'text'
    p.summary                          AS summary,         -- optional
    COALESCE(t.tags, '{}')             AS tags,            -- text[]; optional
    'blog'                             AS section,         -- optional facet
    'en'                               AS lang,            -- optional; default = site language
    p.updated_at                       AS updated_at,      -- drives incremental + reaping; NOT NULL
    'public'                           AS visibility,      -- v1: MUST be 'public' for every row
    jsonb_build_object()               AS authz_context    -- opaque; reserved v2; '{}' in v1
FROM blog_posts p
LEFT JOIN ( ... ) t ON ...
WHERE p.status = 'published' AND p.deleted_at IS NULL;
```

**Contract columns** (the view must expose exactly these names; types as shown):

| Column | Type | Required | Notes |
|---|---|---|---|
| `external_id` | text | yes | stable within the source; becomes `source_key` |
| `url` | text | yes | canonical result URL |
| `title` | text | yes | |
| `body` | text | yes | in `body_kind` format |
| `body_kind` | text | yes | `markdown` \| `html` \| `text` |
| `summary` | text | no | nullable |
| `tags` | text[] | no | default `{}` |
| `section` | text | no | facet |
| `lang` | text | no | default site language |
| `updated_at` | timestamptz | yes | incremental + reaping driver |
| `visibility` | text | yes | **v1: hard-refuse any row ≠ `'public'`** (§11) |
| `authz_context` | jsonb | yes | opaque; `'{}'` in v1; consumer-emitted in v2 |

`body_kind` drives extraction: `markdown` → render via `infodancer/markdown` then
strip to text + section by Markdown headings; `html` → `goquery` text + heading
sections; `text` → as-is, single section.

**v1 hard refusal.** Before indexing a source, the indexer asserts every returned row
has `visibility = 'public'`. Any other value aborts that source's run with an error.
Nothing non-public is ever indexed, so nothing non-public can leak. The capability to
index non-public rows arrives in v2 *together with* the visibility hook (§11).

### 4.3 Optional Go `ContentSource`

For content that needs application logic to assemble (not expressible as a view), a
host that *embeds* the module may register an in-process source:

```go
// source.go
type Document struct {
    ExternalID   string
    URL          string
    Title        string
    Body         string
    BodyKind     BodyKind     // Markdown | HTML | Text
    Summary      string
    Tags         []string
    Section      string
    Lang         string
    UpdatedAt    time.Time
    Visibility   string       // v1: must be "public"
    AuthzContext json.RawMessage // opaque; "{}" in v1
}

type ContentSource interface {
    Name() string
    // Iterate yields documents changed since `since` (zero => full).
    Iterate(ctx context.Context, since time.Time) iter.Seq2[Document, error]
}
```

Both the view contract and `ContentSource` produce the same `Document` shape and flow
through the identical indexing pipeline. The view contract is primary because it works
from the standalone CLI without linking host code.

### 4.4 In-process push API (implemented v1 path)

The v1 ingestion path is **push, not pull**: `search` is consumed as a library and the
host calls in-process methods when its content changes — there is no HTTP ingest
surface. This inverts §4.3's `ContentSource.Iterate(since)` (which the indexer would
poll) into methods the host invokes on a create/edit/delete, which fits an
event-driven website far better than periodic polling. The same `Document` shape and
the same §5 pipeline are used.

```go
func (m *Module) Index(ctx, IngestDocument) (IndexResult, error)        // event-driven create/edit
func (m *Module) IndexBatch(ctx, []IngestDocument) error
func (m *Module) Reconcile(ctx, source, sourceName, []IngestDocument, ReconcileOptions) (ReconcileResult, error)
func (m *Module) Delete(ctx, source, sourceName, externalID string) (bool, error)
```

- `IngestDocument` is the §4.3 `Document` plus `Source`/`SourceName` (its natural key is
  `(Source, SourceName, ExternalID)`).
- **`Reconcile`** is the push analogue of §5.4 reconciliation, for the "SELECT the rows
  we want searchable and hand them over" case: it upserts the supplied set and
  soft-deletes any live document in the source now absent, under the §5.4 bulk-reap
  guard. Per-document `Index`/`Delete` alone cannot detect a row deleted upstream;
  `Reconcile` closes that drift.
- **Embedding is synchronous but non-fatal.** The upsert makes the document
  FTS-searchable immediately (the generated `tsv`); section embedding then runs inline,
  and an embedder outage degrades the document to FTS-only (logged) rather than failing
  the host's write. A host that does not want embed latency on its request path calls
  `Index` from its own goroutine.

**Two pools in-process (§10 invariant preserved).** `Config.DB` is the read-only query
pool the handlers use; the optional `Config.IndexerDB` is the read-write pool the
ingestion methods use. The query handlers physically cannot write, and a serve-only host
omits `IndexerDB` (ingestion then returns `ErrIndexingDisabled`) — so the serving
surface keeps no write path even when one process both serves and ingests.

The §4.1 Hugo `public/` crawl is not yet built; a filesystem CLI (`search index --dir`)
ingests a directory of Markdown files through the same API in the meantime.

---

## 5. Indexing pipeline

`search index` runs as a single batch job. Per source (Hugo crawl + each registered
DB view / ContentSource):

```
discover → extract → section → chunk → hash → upsert document → embed chunks → reconcile
```

### 5.1 Incremental indexing

Each document's `content_hash` is `sha256` over a normalized projection (title +
body_text + tags + url + visibility + authz_context). On upsert, if the stored hash
matches, the document and its chunks are left untouched (and not re-embedded). The
hash, not `updated_at` alone, is authoritative — `updated_at` is only a cheap
pre-filter for DB sources.

### 5.2 Sectioning and chunking

Chunking **splits on Markdown / HTML headings**. Both extractors emit a common model:

```go
type Section struct {
    HeadingPath string // breadcrumb, e.g. "Combat > Initiative"
    Text        string // plain text under this heading, before the next same-or-higher heading
}
```

Each `Section` becomes one chunk, in document order, with `heading_path` preserved
for snippet breadcrumbs. Two refinements:

- **Oversized sections.** A section whose estimated tokens exceed the model's limit
  (via `embedding.LookupLimits`) is split into multiple chunks on paragraph
  boundaries with a small overlap, so the tail of a long section is not silently
  truncated by the embedder. The sub-chunks share the section's `heading_path`.
- **Preamble.** Text before the first heading is its own chunk with an empty
  `heading_path`.

### 5.3 Embeddings (`go-embedding`)

The module **reuses `github.com/matthewjhunter/go-embedding`** and does not define its
own embedder. The backend (Ollama / OpenAI-compatible), model, and distance metric are
operator config — **nothing model-specific is baked into the module.** `nomic-embed-text`
appears in this document only as an illustrative example; a goal of `go-embedding` is to
stay agnostic across embedding models, and `search` must not undermine that with
model-specific defaults. Model-correctness comes from the library, not from us: task
prefixes are produced by `go-embedding`'s per-model formatters (a no-op pass-through for
models with no registered convention), so we never hardcode a prompt string.

**Asymmetric formatting is mandatory.** Document chunks and queries are embedded with
different task prefixes — embedding both sides identically silently degrades ranking:

- Index: `embedding.FormatRetrievalDocument(model, doc.Title, fields, chunk.Text)`
  (nomic emits `search_document:`). `fields` carry section/tags as labeled metadata.
- Query: `embedding.FormatForTask(model, embedding.TaskRetrievalQuery, q)`
  (nomic emits `search_query:`).

Indexing uses `embedding.BatchEmbed` with retry. The distance operator follows the
configured `embed_metric` (default cosine `<=>`; see §3.2).

**Fingerprint + dimension bootstrap.** `embedding.Fingerprint{Model, Dim}` is
persisted in `search_index_meta`. `Dim` is 0 until the first successful `Embed`, so:

1. First-ever index run: do one probe `Embed`, learn `Dim`, write
   `embed_model`/`embed_dim`, then `ALTER` `search_chunks.embedding` to `vector(Dim)`
   and build the HNSW index.
2. Every subsequent run: `embedding.CheckFingerprint(stored, current)`. A mismatch
   (model swap, version/tag change, dim change) means the stored vectors are
   incompatible garbage — the run **refuses** unless `--reembed` is passed, which
   drops and recomputes all embeddings.

If no embedder is configured, indexing skips the embed step and the corpus is
FTS-only. Embeddings can be added later by running with an embedder (a full embed
pass over existing documents).

### 5.4 Reconciliation — soft delete

After a source is fully processed, the indexer reconciles the corpus against the
run's **manifest** (the set of `source_key`s it saw for that source):

- Documents present in the manifest: upserted (hash-gated).
- Documents absent from the manifest: **soft-deleted** (`deleted_at = now()`). Their
  chunks remain (excluded from queries via the live-document join).
- A previously soft-deleted document that reappears: `deleted_at` cleared, embeddings
  preserved (no re-embed).

**Bulk-reap abort guard.** If a single run would soft-delete more than a configurable
fraction of a source's live documents (default 20%), the run aborts that source with
an error and reaps nothing — this catches the empty-`public/`-build and
erroring-view failure modes before they wipe the index. Overridable with
`--allow-bulk-reap` for intentional large removals.

**Hard-delete GC.** A separate maintenance pass (`search index --gc` or scheduled)
hard-`DELETE`s documents soft-deleted longer than a retention window (default 30
days), reclaiming space and keeping the GIN/HNSW indexes from accumulating dead
tuples. Soft delete buys recoverability; GC bounds the cost.

Rationale for soft-delete-by-default: the indexer reconciles against transiently
unreliable external state, and embeddings are the expensive artifact. A bad run
becomes a recoverable no-op instead of an expensive full re-embed.

---

## 6. Query and ranking

### 6.1 FTS arm

```sql
SELECT id, ts_rank_cd(tsv, q) AS rank
FROM search_documents, websearch_to_tsquery('english', $1) q
WHERE tsv @@ q AND deleted_at IS NULL
ORDER BY rank DESC
LIMIT $fanout;            -- default 100
```

`websearch_to_tsquery` parses user input safely (no FTS-syntax injection).

### 6.0 Result pipeline (stages)

The query path is an ordered sequence of stages over a bounded candidate pool. v1
runs only the unbracketed stages; the bracketed stages are reserved (see §8) and slot
in without reworking the pool/pagination model:

```
retrieve (FTS arm ∪ vector arm)
  → RRF fuse                        (§6.3; FTS-only deployments skip the vector arm)
  → [ v2: re-rank top-N ]           (optional; reorders the fused candidates)
  → [ v2: visibility filter ]       (optional; drops candidates + revises counts, §11.2)
  → keyset paginate                 (§6.6)
```

Because pagination is keyset over the *computed* order of a bounded pool — not `OFFSET`
over a raw query — inserting the rerank or visibility stage changes only the ordering
and membership of the pool, never the pagination mechanism.

### 6.2 Vector arm (when an embedder is configured)

The query is embedded once (cached, §6.4), then the best chunk per document:

```sql
SELECT c.document_id, min(c.embedding <=> $qvec) AS dist  -- operator follows embed_metric
FROM search_chunks c
JOIN search_documents d ON d.id = c.document_id
WHERE d.deleted_at IS NULL
GROUP BY c.document_id
ORDER BY dist
LIMIT $fanout;           -- default 100
```

### 6.3 Reciprocal Rank Fusion

The two arms are fused by RRF over a bounded candidate pool:

```
score(doc) = Σ_arm  1 / (k + rank_arm(doc))      k = 60 (configurable)
```

`rank_arm` is the 1-based position within that arm; a document missing from an arm
contributes nothing for it. Fusion is computed in the application over the candidate
pool (≤ `2 × fanout` documents). FTS-only deployments skip the vector arm and rank by
`ts_rank_cd` directly.

When a re-ranker is configured (§8, v2), RRF still selects the candidate pool; the
re-ranker reorders its top `RerankTopN` and its scores become the final order. When no
re-ranker is configured (v1, and any deployment that opts out), the RRF order is final.

### 6.4 Query embedding cache

The server holds an `embedding.QueryCache` (LRU, default 1024 entries) keyed on the
normalized, task-formatted query, so repeated/typeahead queries reuse vectors.

### 6.5 Snippets

For each result, a highlighted snippet via
`ts_headline('english', body_text, websearch_to_tsquery(...))`, falling back to the
stored `snippet`. `ts_headline` output is escaped on render; we never emit stored
HTML.

### 6.6 Pagination, counts, facets

- **Keyset/cursor pagination** over the stably-sorted fused list. The cursor is an
  opaque base64 of `(last_score, last_doc_id)`. Naive `OFFSET` is *not* used —
  this is what lets v2 visibility filtering (drop-then-page) slot in without
  reworking the query layer. Deep pagination beyond the candidate pool is out of
  scope for v1 (documented limit).
- **Counts and facets** (source / section / tag) are computed **post-filter**, over
  the candidate set the user is actually allowed to see. In v1 every row is public so
  this is trivially the full set; the discipline exists so v2 filtering revises totals
  correctly rather than leaking pre-filter counts.

---

## 7. Serving — HTTP surface

Routes (relative to `BasePath`, default `/search`):

| Method | Path | Purpose |
|---|---|---|
| GET | `/` | Search page shell: input + empty results region. |
| GET | `/search` | **HTMX partial.** Renders result cards for `?q=&cursor=&source=&section=&tag=`. Triggered by `hx-get` on `keyup changed delay:200ms` and on facet clicks. |
| GET | `/static/*` | Embedded assets + `infodancer/ui` assets (overlayable). |
| GET | `/healthz` | Liveness/readiness (checks DB + meta row). |
| GET | `/opensearch.xml` | OpenSearch description (nice-to-have; may land v1.x). |

**UI.** A search box that swaps a results partial into a target div. Result cards show
title, highlighted snippet, source badge, section/tags, and the canonical link;
facet chips filter by source/section/tag. Chrome consumes `infodancer/ui` partials and
`--app-*` tokens, with a standalone fallback so `search serve` is usable on its own —
the same pattern as `faq`.

**CSRF.** Search is read-only (GET), so the bundled same-origin guard (reused from the
`faq` pattern) primarily matters if a host mounts state-changing admin endpoints
later; it is present and configurable (`CSRFConfig.Disable`).

---

## 8. Library API

```go
// search.go
type Module struct { /* ... */ }

func New(cfg Config) (*Module, error)          // validates DB; applies defaults
func (m *Module) Routes() http.Handler         // mount under BasePath with StripPrefix
func (m *Module) Migrate(ctx context.Context) error  // goose migrations (RW role)
```

```go
// config.go — serving configuration (mirrors faq.Config)
type Config struct {
    DB       *pgxpool.Pool        // required (RO query role when standalone)
    Embedder embedding.Embedder   // optional; nil => FTS-only
    Reranker Reranker             // optional; nil => no rerank stage (reserved; see below)
    RerankTopN int                // how many fused candidates to rerank when Reranker set; 0 in v1
    Logger   *slog.Logger         // default slog.Default()
    BasePath string               // default "/search"
    SiteName string               // chrome brand
    HomeURL  string               // default "/"
    LogoURL  string
    Templates fs.FS               // optional overlay over embedded
    Static    fs.FS               // optional overlay
    CSRF      CSRFConfig
    PageSize  int                 // default 20, clamped [1,100]
    Fanout    int                 // candidate pool per arm, default 100
    RRFk      int                 // default 60
    QueryCacheSize int            // default 1024
    AutoMigrate bool              // default false
}
```

```go
// reranker.go — RESERVED for v2. go-embedding is adding experimental re-ranker
// support; when it ships, this alias adopts the library's interface rather than
// defining a competing one. Shape (anticipated): score (query, candidate-text)
// pairs and return them reordered. Optional everywhere — nil falls back to the RRF
// order, so a deployment without a reranker (or running a model/backend that offers
// none) behaves exactly as it does today.
type Reranker interface {
    Rerank(ctx context.Context, query string, candidates []RerankItem) ([]RerankItem, error)
}
type RerankItem struct { DocID int64; Text string; Score float64 }
```

```go
// user.go — RESERVED for v2; present so adding it later is not a breaking change.
type VisibilityState int            // Omit (zero) | Tombstone | Show
type Viewer struct { /* host-supplied identity; opaque to v1 */ }
type Decision struct { DocID int64; State VisibilityState }
// Batched, three-state. Registered per source. Always nil in v1.
type VisibilityFilter func(ctx context.Context, v Viewer, candidates []int64, authz [][]byte) []Decision
```

Source registration carries the reserved hook field now:

```go
type Source struct {
    Name   string             // reads view search_source_<Name>
    Filter VisibilityFilter   // nil in v1
}
func (m *Module) RegisterSource(s Source)            // for embedded hosts
func (m *Module) RegisterContentSource(cs ContentSource, f VisibilityFilter)
```

---

## 9. CLI

A TOML config file with flag overrides. DSNs and the embedding backend may also come
from the environment (`embedding.ConfigFromEnv`).

```
search index   --config search.toml [--hugo DIR] [--public DIR] [--db DSN]
               [--reembed] [--gc] [--dry-run] [--allow-bulk-reap]
               [--max-reap-fraction 0.2]

search serve   --config search.toml [--addr :8080] [--db DSN] [--base-path /search]
```

```toml
# search.toml
base_url_from_hugo = true          # else set base_url explicitly
hugo_dir   = "."
public_dir = "./public"
content_selector = ["main", "article", ".content"]

[db]
indexer_dsn = "postgres://search_rw@/sitedb"   # RW role, used by `index`
query_dsn   = "postgres://search_ro@/sitedb"    # RO role, used by `serve`

[embedding]                          # omit entirely for FTS-only
backend = "ollama"
base_url = "http://localhost:11434"
model = "nomic-embed-text"

sources = ["blog", "faq"]            # reads search_source_blog, search_source_faq

[reaping]
max_fraction = 0.2
gc_retention_days = 30
```

---

## 10. Security

- **Two least-privilege roles.** RW indexer (DDL + DML on search tables) vs RO query
  (`SELECT` only). The serving surface cannot write. Operator provisions both.
- **Injection.** All queries via `sqlc`/`pgx` parameter binding;
  `websearch_to_tsquery` for user FTS input; no string-built SQL.
- **No third-party HTML served.** Only extracted plain text and our own escaped
  snippets are stored/rendered. Hugo page HTML and DB markdown are reduced to text at
  index time. DB Markdown rendered for extraction goes through `infodancer/markdown`
  (goldmark + bluemonday) — the same sanitization boundary used elsewhere.
- **Vendored client assets.** htmx vendored, pinned, SRI — no CDN (mirrors `mdedit`).
- **Same-origin CSRF guard** available (faq pattern).
- **Secrets** (DSNs, API keys) come from config/env, never committed; `SECURITY.md`
  documents handling. CI runs `govulncheck`.
- **DoS posture.** `PageSize`/`Fanout` clamps; query timeouts via context; HNSW
  `ef_search` bounded.

---

## 11. Access control

### 11.1 v1 — public-only, hard refusal

The search endpoint is unauthenticated and applies no per-user filtering. The indexer
**refuses** any DB-source row whose `visibility ≠ 'public'` and never indexes Hugo
drafts. It is impossible to leak what was never indexed.

### 11.2 v2 — per-identity visibility hook (forward-compat, reserved now)

OSG is the motivating case and the reason role columns alone are insufficient. In
OSG, whether a viewer may see a session note is **relational**: "members" means *an
active membership row on the specific campaign the note belongs to*, not a global
role. Only the consumer can compute that. (See `osg .../notevisibility` and
`.../permissions`.) Three properties drive the reservations:

1. The visibility decision is a host callback keyed on **identity + the document's
   opaque context**, not a search-owned role string.
2. The decision is **three-state** — `Show` / `Tombstone` / `Omit` — where `Omit`
   must remove a row from candidates **and totals and snippets together**. OSG's
   explicit threat is "an anonymous viewer counts the members-only rows"; for search a
   leaked *count* or a "1 result vs no results" delta leaks existence.
3. The check needs the viewer's memberships loaded **once** and applied to a
   **batch** of candidates — per-row callbacks with per-row lookups are the perf trap.

v1 reservations that make v2 additive (no re-index, no breaking change):

- **`authz_context jsonb` is carried on every DB-sourced document now** (OSG would set
  `{campaign_id, visibility}`), included in the v1 view contract though unused.
- **The engine is visibility-agnostic**; "public-only" lives in the consumer's view
  `WHERE` clause, not as a structural assumption that everything indexed is
  world-readable.
- **Keyset pagination + over-fetch-then-filter** candidate model (§6.6).
- **Post-filter counts/facets** (§6.6).
- **The reserved batched, three-state `VisibilityFilter`** hook field on `Source`
  (§8), `nil` in v1.

v2 is then one change: widen the consumer views to expose non-public rows + their
`authz_context`, flip the off-by-default `index_nonpublic` flag, and supply the
`VisibilityFilter`. The flag and the hook **ship together** — never the flag alone.

---

## 12. Testing strategy

Test-driven, per standing preference.

- **Unit (pure Go, no DB):** Hugo `public/`→URL derivation (incl. ugly URLs, index
  pages, sections); HTML/Markdown sectioners and chunker (heading splits, oversized
  split, preamble); RRF fusion math; cursor encode/decode; config reader layering;
  the v1 visibility hard-refusal; (v2) the three-state filter semantics including
  `Omit`-from-counts.
- **Integration (real Postgres + `pgvector`, testcontainers):** migrations + dim
  bootstrap; incremental hash gating; soft-delete reconciliation + bulk-reap abort +
  GC; FTS ranking; vector + RRF with a **fake `Embedder`** (deterministic vectors);
  fingerprint-mismatch refusal and `--reembed`.
- **HTTP:** handler tests for the search partial, facets, pagination, empty/whitespace
  queries.
- **Security:** an XSS corpus asserting no executable HTML survives into rendered
  results (precedent: `markdown`/`mdedit`); a test that a `visibility != 'public'`
  row aborts indexing.
- **Leak/perf:** `goleak` on the server; a benchmark on the fusion + query path.

---

## 13. Build, CI, tooling

- `go.work` member during in-tree development; consumed by tag + `go mod vendor`.
- CI: `gofmt`, `go vet`, `go test -race`, `golangci-lint`, `govulncheck` — green at
  every tag, mirroring the suite.
- `modernize` clean; Go 1.25 idioms (`iter.Seq2`, `slices`/`maps`, range-over-func).

---

## 14. Milestones

- **M0** — repo scaffold, `go.mod`, CI, `go.work`, this DESIGN.md.
- **M1** — schema + migrations + `sqlc` store; meta singleton; dim bootstrap.
- **M2** — Hugo ingestion: config reader, URL derivation, HTML extraction, frontmatter,
  sectioner. FTS-only index of a Hugo site end to end.
- **M3** — query layer: FTS arm, snippets, keyset pagination, facets, counts.
- **M4** — HTMX serving + `infodancer/ui` chrome + standalone `serve`.
- **M5** — DB view contract ingestion + `infodancer/markdown` extraction + v1 hard
  refusal.
- **M6** — embeddings: `go-embedding` wiring, chunk embedding, fingerprint/dim,
  vector arm, RRF, query cache, `--reembed`.
- **M7** — reconciliation: soft delete, bulk-reap guard, GC.
- **M8** — CLI/config polish, OpenSearch, docs (README/SECURITY/CONTRIBUTING), tag
  `v0.1.0`. First production target: a matthewjhunter Hugo site; then `blog`/`osg`.

---

## 15. Deferred / future work

- v2 per-identity visibility hook + `index_nonpublic` flag (§11.2).
- **Optional re-ranker stage** (§6.0, §8), adopting `go-embedding`'s experimental
  re-ranker once released. Strictly optional: present ⇒ reorder the fused top-N;
  absent ⇒ fall back to the RRF order. No schema or pagination change required.
- **Embedding-model agnosticism** stays a standing constraint, not just a v1 default:
  no model-specific constants anywhere; model, backend, and distance metric are
  operator config; per-model behavior (prompts, limits) is delegated to `go-embedding`.
- Multilingual indexing (per-row FTS config; replaces the fixed generated column).
- In-page result preview pane (DB markdown via `infodancer/markdown`).
- Image/media awareness in results.
- Suggestions / "did you mean" / synonyms.
- Remote/HTTP content feeds, if a consumer ever needs cross-service indexing.
