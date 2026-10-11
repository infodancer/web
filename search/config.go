package search

import (
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/infodancer/ui"
	"github.com/jackc/pgx/v5/pgxpool"
	embedding "github.com/matthewjhunter/go-embedding"

	"github.com/infodancer/web/search/internal/query"
)

// Config is the serving configuration for the module. It mirrors the shape in
// DESIGN §8; the embeddings-milestone fields (Embedder, RRFk, QueryCacheSize)
// are omitted until the vector arm is built, and added then without disturbing
// what is here.
type Config struct {
	// DB is the Postgres connection pool used by the serving query path.
	// Required. When standalone this is the read-only query role; an embedding
	// host may pass its own pool. The query handlers only ever read through it.
	DB *pgxpool.Pool

	// IndexerDB is the read-write pool used by the ingestion methods
	// (Index/IndexBatch/Reconcile/Delete). It is optional: leaving it nil builds
	// a serve-only module whose ingestion methods return ErrIndexingDisabled, so
	// the serving surface has no write path (DESIGN §10). A host that both serves
	// and ingests in one process supplies both pools — typically a read-only role
	// for DB and the indexer role here — keeping the privilege split at the
	// connection level even in-process.
	IndexerDB *pgxpool.Pool

	// Reranker is an optional second-stage reranker. nil (the default) makes the
	// rerank stage a pass-through and the FTS order final (DESIGN §6.0/§8).
	Reranker embedding.Reranker

	// Embedder enables vector indexing and the vector query arm. nil (the
	// default) keeps the corpus FTS-only. When set, each changed document's
	// sections are embedded and stored as chunks at index time, and queries fuse
	// a vector arm with FTS via RRF (DESIGN §5.3/§6). Nothing model-specific is
	// baked in — the backend, model, and metric are this caller's choice.
	Embedder embedding.Embedder

	// ChunkTokens is the token target one chunk is sized to. Defaults to 512.
	//
	// The model's full context window is the wrong target and the library says
	// so: a vector has fixed capacity, so filling the window averages away the
	// specificity retrieval depends on. 256-512 tokens is the working range.
	// The model's registered byte ceiling still applies on top -- whichever
	// binds first wins, so this can only ever make chunks smaller.
	//
	// Changing it moves the recipe fingerprint (DESIGN §5.3), which clears the
	// stored vectors so a backfill rebuilds them at the new size.
	ChunkTokens int

	// EmbedBatchSize is how many chunks are sent per embed request at index
	// time. Defaults to 25, which both backend families handle without tripping
	// a per-request size limit. It affects throughput only, never the vectors.
	EmbedBatchSize int

	// EmbedMetric is the vector distance metric: "cosine" (default), "ip", or
	// "l2". It sets both the HNSW operator class at bootstrap and the query
	// distance operator; changing it after the first index run requires a rebuild.
	EmbedMetric string

	// ReembedOnMismatch, when true, lets an embedding model/dimension change drop
	// and recompute all embeddings instead of refusing (DESIGN §5.3). Off by
	// default so a silent model swap cannot quietly degrade rankings.
	ReembedOnMismatch bool

	// RerankTopN bounds how many of the fused candidates the reranker rescores.
	// Defaults to 50; ignored when Reranker is nil.
	RerankTopN int

	// RRFk is the reciprocal-rank-fusion constant (DESIGN §6.3). Defaults to 60.
	RRFk int

	// QueryCacheSize bounds the LRU of query-embedding vectors (DESIGN §6.4).
	// Defaults to 1024; ignored when Embedder is nil.
	QueryCacheSize int

	// BackendTimeout bounds each call to the embedding or rerank backend made
	// while serving a query. A backend that has not answered within it is
	// treated as unavailable and the query degrades (FTS-only, or fused order)
	// instead of waiting out the request; the index path is not affected.
	// Defaults to 3s; a negative value disables the budget and leaves the
	// request context in charge.
	BackendTimeout time.Duration

	// Logger is the module-level structured logger. Defaults to slog.Default.
	Logger *slog.Logger

	// BasePath is the URL prefix the module is mounted at. Defaults to "/search".
	BasePath string

	// SiteName is the chrome brand. Defaults to "Search" when empty.
	SiteName string

	// HomeURL is where the brand link points. Defaults to "/".
	HomeURL string

	// LogoURL is an optional brand mark in the nav.
	LogoURL string

	// Chrome, when set, supplies the full host page chrome (nav, footer, meta,
	// theme, analytics, asset base, head tags) per request as a ui.DocumentData
	// that pages render through ui/document. A host site sets this to inject its
	// own live, auth-aware navigation so search matches the rest of the site.
	// nil keeps the SiteName/HomeURL-derived default chrome, so a standalone
	// `search serve` still renders a complete page.
	Chrome func(*http.Request) ui.DocumentData

	// Templates and Static optionally overlay the embedded defaults: a
	// same-named file in the overlay wins over the module's bundled copy.
	Templates fs.FS
	Static    fs.FS

	// CSRF tunes (or disables) the bundled same-origin guard.
	CSRF CSRFConfig

	// PageSize is how many results a query returns. Defaults to 20; clamped to
	// [1,100] by the handler.
	PageSize int

	// Fanout bounds the candidate pool retrieved per query (the rerank stage
	// reorders within it). Defaults to 100.
	Fanout int

	// AutoMigrate, if true, runs the embedded migrations during New.
	AutoMigrate bool
}

// defaultChunkTokens is the token target one chunk is sized to. 512 is the top
// of the 256-512 range that retrieval actually works in, and well under the
// 2048-token window a typical embedder advertises.
const defaultChunkTokens = 512

// defaultEmbedBatchSize matches go-embedding's own default: large enough to
// amortise the round trip, small enough that neither backend family rejects the
// request.
const defaultEmbedBatchSize = 25

// CSRFConfig tunes the bundled same-origin CSRF middleware.
type CSRFConfig struct {
	// Disable skips the guard. Use only when the host enforces an equivalent
	// guard at a higher layer.
	Disable bool
}

func (c *Config) applyDefaults() {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.BasePath == "" {
		c.BasePath = "/search"
	}
	if c.HomeURL == "" {
		c.HomeURL = "/"
	}
	if c.SiteName == "" {
		c.SiteName = "Search"
	}
	if c.PageSize == 0 {
		c.PageSize = 20
	}
	if c.Fanout == 0 {
		c.Fanout = 100
	}
	if c.EmbedMetric == "" {
		c.EmbedMetric = "cosine"
	}
	if c.RerankTopN == 0 {
		c.RerankTopN = 50
	}
	if c.RRFk == 0 {
		c.RRFk = 60
	}
	if c.QueryCacheSize == 0 {
		c.QueryCacheSize = 1024
	}
	if c.BackendTimeout == 0 {
		c.BackendTimeout = query.DefaultBackendTimeout
	}
	if c.ChunkTokens == 0 {
		c.ChunkTokens = defaultChunkTokens
	}
	if c.EmbedBatchSize == 0 {
		c.EmbedBatchSize = defaultEmbedBatchSize
	}
}
