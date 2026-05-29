-- Dev seed fixture: sample documents for exercising the search UI while the
-- indexer is unbuilt. Idempotent (upsert on the natural key). content_hash is a
-- throwaway md5 here; the real indexer computes the sha256 projection (DESIGN
-- §5.1). All rows are visibility='public' (the v1 invariant, DESIGN §11.1).
--
-- Load with: search seed --db <indexer_dsn>

INSERT INTO search_documents
    (source, source_name, source_key, url, title, section, tags, headings, summary, snippet, body_text, content_hash, published_at)
VALUES
('hugo', '', 'posts/postgres-fts/', '/posts/postgres-fts/',
 'Full-text search in Postgres without a separate engine',
 'posts', ARRAY['postgres','search','sql'],
 'tsvector and tsquery  Ranking with ts_rank_cd  GIN indexes',
 'Why Postgres full-text search is usually enough, and how tsvector, GIN indexes, and ts_rank_cd fit together.',
 'Postgres ships a capable full-text search engine; reach for Elasticsearch only when you have outgrown it.',
 'Postgres ships a capable full-text search engine built on tsvector and tsquery. A generated tsvector column indexed with GIN gives fast matching, and ts_rank_cd ranks results by term proximity and frequency. websearch_to_tsquery parses user input safely, with no query-syntax injection surface. For most applications this is enough; reach for a dedicated engine like Elasticsearch only when you have genuinely outgrown what the database can do.',
 decode(md5('posts/postgres-fts/'), 'hex'), now() - interval '20 days'),

('hugo', '', 'posts/pgvector-hybrid/', '/posts/pgvector-hybrid/',
 'Hybrid search: fusing full-text and vector results with RRF',
 'posts', ARRAY['postgres','pgvector','embeddings','search'],
 'The two arms  Reciprocal Rank Fusion  When reranking helps',
 'Combining a full-text arm and a pgvector arm with Reciprocal Rank Fusion, and where a cross-encoder reranker fits.',
 'Hybrid search runs a lexical arm and a semantic arm and fuses them with Reciprocal Rank Fusion.',
 'Hybrid search runs two retrieval arms over the same corpus: a lexical full-text arm and a semantic vector arm backed by pgvector. Reciprocal Rank Fusion combines their rankings without needing comparable scores. A cross-encoder reranker can reorder the fused candidates for a further quality bump, but it is optional: with no reranker configured the fused order stands.',
 decode(md5('posts/pgvector-hybrid/'), 'hex'), now() - interval '12 days'),

('hugo', '', 'posts/homelab-postgres/', '/posts/homelab-postgres/',
 'Running Postgres in the homelab',
 'homelab', ARRAY['homelab','postgres','docker'],
 'Container setup  Backups with pg_dump  Least-privilege roles',
 'Notes on running a small Postgres instance in a homelab: containers, backups, and least-privilege roles.',
 'A small homelab Postgres needs three things: a backup that actually restores, monitoring, and least-privilege roles.',
 'A homelab Postgres does not need to be elaborate, but it does need three things done right: a backup you have actually tested restoring, basic monitoring, and least-privilege roles so a compromised application cannot drop your tables. Run it in a container with a named volume, schedule pg_dump to object storage, and give each app its own role with only the grants it needs.',
 decode(md5('posts/homelab-postgres/'), 'hex'), now() - interval '40 days'),

('hugo', '', 'posts/go-htmx/', '/posts/go-htmx/',
 'Server-rendered search UIs with Go and htmx',
 'posts', ARRAY['go','htmx','web'],
 'Why htmx  The results partial  Progressive enhancement',
 'Building a responsive search UI with Go templates and htmx, with a partial that swaps in as you type.',
 'htmx lets a Go server render a search-results fragment and swap it into the page on keyup, no SPA required.',
 'htmx turns hypertext attributes into AJAX, so a Go server can render an HTML fragment and swap it into the page without a single-page-app framework. A search box fires an hx-get on keyup with a short debounce, the server returns just the results partial, and htmx replaces the target div. Because the same endpoint also renders a full page server-side, the UI degrades gracefully when JavaScript is off.',
 decode(md5('posts/go-htmx/'), 'hex'), now() - interval '5 days'),

('db', 'blog', '101', '/blog/securing-search-endpoints',
 'Securing a public search endpoint',
 'blog', ARRAY['security','search','web'],
 'Injection-safe queries  No third-party HTML  DoS clamps',
 'The security posture of a public search endpoint: parameterized queries, plain-text-only rendering, and request clamps.',
 'A search endpoint is an injection magnet; parameterize everything, store plain text, and clamp page size and fanout.',
 'A public search endpoint is an injection magnet and an amplification risk. Parameterize every query and parse user terms with websearch_to_tsquery so there is no FTS-syntax injection. Store and render only extracted plain text plus your own escaped highlight markup, never third-party HTML, which removes the result-rendering cross-site-scripting surface. Clamp page size and candidate fanout, and put a timeout on every query to bound the cost of a hostile request.',
 decode(md5('blog:101'), 'hex'), now() - interval '2 days'),

('db', 'blog', '102', '/blog/reranking-explained',
 'What a reranker actually does',
 'blog', ARRAY['embeddings','search','ai'],
 'First-stage retrieval  Cross-encoders  Graceful degradation',
 'A plain explanation of second-stage reranking: cross-encoders, when they help, and degrading to first-stage order.',
 'A reranker rescores a shortlist with a cross-encoder; if the backend is down, you keep the first-stage order.',
 'First-stage retrieval (full-text, vectors, or both) is tuned for recall over a large corpus. A reranker takes the resulting shortlist and rescores each query-document pair with a cross-encoder that reads both together, which is far more accurate than comparing independent embeddings. It is a quality-versus-latency trade: run it on the top few dozen candidates, not the whole corpus. If the rerank backend is unavailable, a well-built pipeline degrades to the first-stage order rather than failing the query.',
 decode(md5('blog:102'), 'hex'), now() - interval '1 day')

ON CONFLICT (source, source_name, source_key) DO UPDATE SET
    url          = EXCLUDED.url,
    title        = EXCLUDED.title,
    section      = EXCLUDED.section,
    tags         = EXCLUDED.tags,
    headings     = EXCLUDED.headings,
    summary      = EXCLUDED.summary,
    snippet      = EXCLUDED.snippet,
    body_text    = EXCLUDED.body_text,
    content_hash = EXCLUDED.content_hash,
    published_at = EXCLUDED.published_at,
    deleted_at   = NULL,
    updated_at   = now();
