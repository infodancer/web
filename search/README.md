# infodancer/search

A reusable search tool for the InfoDancer / matthewjhunter web properties.
Postgres-backed full-text search with an HTMX UI that consumes the
`infodancer/ui` chrome. See [DESIGN.md](DESIGN.md) for the authoritative
specification.

> **Status.** Serving and ingestion both work. A host application embeds the
> module, calls the in-process ingestion API when its content changes, and
> mounts the search UI. Full-text search works immediately; supplying an
> embedder adds vector search fused with FTS (RRF) and an optional reranker.
> Long sections are split to a token budget (`Config.ChunkTokens`, default 512)
> and embedded in batches. Not yet built: the Hugo `public/` crawl source.

## Ingestion (in-process)

`search` is consumed as a Go library. The host calls these methods when a
document is created, edited, or removed — there is no HTTP ingest surface.
Document bodies are Markdown, HTML, or plain text; only extracted plain text and
escaped snippets are ever stored or served, never the source markup.

```go
m, _ := search.New(search.Config{DB: queryPool, IndexerDB: indexerPool})

// On a create/edit:
m.Index(ctx, search.IngestDocument{
    Source: "blog", SourceName: "posts", ExternalID: "42",
    URL: "/posts/hello", Title: "Hello", Body: "# Hello\n\nworld",
    BodyKind: search.BodyMarkdown,
})

// On a delete:
m.Delete(ctx, "blog", "posts", "42")

// For a bulk pass (e.g. SELECT the rows you want searchable): upserts the set
// and soft-deletes anything now absent, guarded against reaping >20% of a
// source unless ReconcileOptions.AllowBulkReap is set.
m.Reconcile(ctx, "blog", "posts", docs, search.ReconcileOptions{})
```

`Index` is hash-gated: unchanged content is skipped (and not re-embedded) —
`content_hash` is the incremental-indexing cache. Ingestion requires the
read-write `Config.IndexerDB`; a serve-only module leaves it nil and the
ingestion methods return `ErrIndexingDisabled`, so the query surface keeps no
write path even in one process.

## Serving

- `search serve` — long-running HTTP server (read-only role). FTS ∪ vector → RRF
  → rerank → results.
- `search index --dir <path>` — filesystem driver: ingest a directory of `*.md`
  files and reconcile against the source. The standalone equivalent of the
  in-process API for filesystem content.
- `search seed` — load `testdata/seed.sql` sample documents.
- Library mount: `search.New(cfg).Routes()`.

### Skinning (host chrome)

Pages render through infodancer/ui's `ui/document`. A host site owns the chrome
by setting `Config.Chrome`, called per request to supply a `ui.DocumentData`
(nav, footer, meta, theme, analytics, asset base, head tags) — so search wears
the host's live, auth-aware navigation instead of the module's. Leave it nil for
a standalone deployment and the module renders a complete default page from
`SiteName`/`HomeURL`. The module never invents chrome; it owns the search
content (the page's `content`/`title`/`head` blocks) and nothing else.

```go
m, _ := search.New(search.Config{
    DB:     pool,
    Chrome: func(r *http.Request) ui.DocumentData { return host.PageChrome(r) },
})
```

## Embeddings and reranking (optional)

Both are opt-in and read from `go-embedding`'s env. With neither set the corpus
is FTS-only. The backend, model, and metric are operator config — nothing
model-specific is baked in.

```sh
# Vector arm: indexing embeds document sections; queries fuse vector with FTS.
export EMBEDDING_BACKEND=ollama
export EMBEDDING_BASE_URL=http://your-embedder:11434
export EMBEDDING_MODEL=nomic-embed-text       # 768-dim; learned at first index

# Reranker (Cohere/Jina shape, e.g. llama.cpp --reranking):
export RERANK_BACKEND=jina
export RERANK_BASE_URL=http://your-reranker:8012
export RERANK_MODEL=bge-reranker-v2-m3
export RERANK_NORMALIZE_SCORES=true           # set for raw-logit servers
```

The pgvector chunk table and HNSW index are created lazily on the first embed
with the dimension learned from the model, so FTS-only deployments need no
extension. A model/dimension change is refused unless `index --reembed` (or
`Config.ReembedOnMismatch`) is set. If the reranker is unreachable the query
degrades to fused order rather than failing, and if the embedder is
unreachable the query runs FTS-only.

Unreachable includes hung: each backend call made while serving a query
has its own budget, `Config.BackendTimeout` (default 3s), after which the
query degrades the same way instead of waiting out the request. A page
load should never sit behind an inference server that has stopped
answering. Indexing is not subject to it.

## Quick start (local dev)

```sh
# 1. Provision a dev database + roles (Postgres on localhost).
createdb search_dev
psql -d search_dev -c "CREATE ROLE search_rw LOGIN; CREATE ROLE search_ro LOGIN;"

# 2. Seed sample documents (runs migrations first; uses the RW role).
go run ./cmd/search seed --db "postgres:///search_dev"

#    …or ingest a directory of Markdown files:
go run ./cmd/search index --dir ./content --base-url https://example.com --db "postgres:///search_dev"

# 3. Serve (RO role).
go run ./cmd/search serve --db "postgres:///search_dev" --addr :8080
# open http://localhost:8080/search/
```

Local builds use the `infodancer` workspace `go.work` to resolve `go-embedding`,
`infodancer/ui`, and `infodancer/markdown` from sibling working trees.

## License

Apache-2.0.
