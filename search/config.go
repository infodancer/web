package search

import (
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	embedding "github.com/matthewjhunter/go-embedding"
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
}
