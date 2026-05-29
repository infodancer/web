// Package search is a reusable Postgres-backed search module for the InfoDancer
// / matthewjhunter web properties. It serves full-text search results through
// an HTMX UI that consumes the infodancer/ui chrome.
//
// The host mounts a small surface: New, Routes, Migrate.
//
//	m, _ := search.New(search.Config{DB: pool})
//	mux.Handle("/search/", http.StripPrefix("/search", m.Routes()))
//
// See DESIGN.md at the repository root for the full specification. This tree
// implements the serving path; content indexing is specified there but not yet
// built (seed the corpus to exercise the UI).
package search

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/stdlib"
	embedding "github.com/matthewjhunter/go-embedding"
	"github.com/pressly/goose/v3"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/search/internal/handler"
	"github.com/infodancer/web/search/internal/index"
	"github.com/infodancer/web/search/internal/query"
	"github.com/infodancer/web/search/internal/render"
	"github.com/infodancer/web/search/store"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed all:templates
var templatesFS embed.FS

//go:embed all:static
var staticFS embed.FS

// Module is the runtime entry point. Construct with [New] and mount with
// [Module.Routes]. Safe for concurrent use.
type Module struct {
	cfg    Config
	server *handler.Server
	// indexer is the ingestion pipeline, nil unless Config.IndexerDB is set. The
	// ingestion methods return ErrIndexingDisabled when it is nil.
	indexer *index.Indexer
}

// New constructs a Module from cfg. It returns an error if Config.DB is
// missing. If cfg.AutoMigrate is true, New runs the embedded migrations first.
func New(cfg Config) (*Module, error) {
	if cfg.DB == nil {
		return nil, errors.New("search: Config.DB is required")
	}
	cfg.applyDefaults()
	m := &Module{cfg: cfg}

	if cfg.AutoMigrate {
		if err := m.Migrate(context.Background()); err != nil {
			return nil, err
		}
	}

	tpls, err := render.New(templatesFS, cfg.Templates, uiassets.PartialsFS(), cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("search: templates: %w", err)
	}

	staticFiles, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("search: static fs: %w", err)
	}

	if cfg.IndexerDB != nil {
		var (
			emb    index.Embedder
			chunks index.ChunkStore
		)
		if cfg.Embedder != nil {
			emb = embedAdapter{emb: cfg.Embedder}
			chunks = &chunkStore{
				pool:    cfg.IndexerDB,
				model:   cfg.Embedder.Model(),
				metric:  cfg.EmbedMetric,
				reembed: cfg.ReembedOnMismatch,
				log:     cfg.Logger,
			}
		}
		m.indexer = index.New(storeWriter{q: store.NewStore(cfg.IndexerDB)}, emb, chunks, cfg.Logger)
	}

	var qcache *embedding.QueryCache
	if cfg.Embedder != nil {
		qcache = embedding.NewQueryCache(cfg.QueryCacheSize)
	}
	engine := query.New(store.NewStore(cfg.DB), query.Options{
		Reranker:   cfg.Reranker,
		Embedder:   cfg.Embedder,
		QueryCache: qcache,
		Metric:     cfg.EmbedMetric,
		Fanout:     cfg.Fanout,
		RRFk:       cfg.RRFk,
		RerankTopN: cfg.RerankTopN,
		Logger:     cfg.Logger,
	})

	srv, err := handler.New(handler.Deps{
		Engine:       engine,
		Templates:    tpls,
		Logger:       cfg.Logger,
		BasePath:     cfg.BasePath,
		SiteName:     cfg.SiteName,
		HomeURL:      cfg.HomeURL,
		LogoURL:      cfg.LogoURL,
		PageSize:     cfg.PageSize,
		Static:       overlayStatic(withUIAssets(staticFiles), cfg.Static),
		CSRFDisabled: cfg.CSRF.Disable,
	})
	if err != nil {
		return nil, fmt.Errorf("search: handler: %w", err)
	}
	m.server = srv
	return m, nil
}

// Routes returns the module's HTTP router, mounted at BasePath with
// http.StripPrefix so the module sees paths relative to its mount point.
func (m *Module) Routes() http.Handler {
	return m.server.Routes()
}

// overlayStatic prefers files in override, falling back to defaults. nil
// override returns defaults unchanged.
func overlayStatic(defaults, override fs.FS) fs.FS {
	if override == nil {
		return defaults
	}
	return overlayFS{primary: override, fallback: defaults}
}

type overlayFS struct {
	primary, fallback fs.FS
}

func (o overlayFS) Open(name string) (fs.File, error) {
	if f, err := o.primary.Open(name); err == nil {
		return f, nil
	}
	return o.fallback.Open(name)
}

// withUIAssets serves infodancer/ui's CSS at the "ui/" prefix in front of the
// module's own static files, so base.gohtml's <link> tags resolve the shared
// tokens.css/base.css without the host mounting infodancer/ui separately.
func withUIAssets(own fs.FS) fs.FS {
	return uiRoutedFS{own: own, ui: uiassets.AssetsFS()}
}

type uiRoutedFS struct {
	own, ui fs.FS
}

func (u uiRoutedFS) Open(name string) (fs.File, error) {
	if name == "ui" || strings.HasPrefix(name, "ui/") {
		return u.ui.Open(strings.TrimPrefix(strings.TrimPrefix(name, "ui"), "/"))
	}
	return u.own.Open(name)
}

// migrationsTableName is the dedicated goose version table for the search
// module — distinct from the default so search's migration history does not
// collide with a host application's own goose tree when they share a database.
const migrationsTableName = "search_goose_db_version"

// Migrate runs the embedded goose migrations against the configured DB. Safe to
// call repeatedly. Uses goose.Provider (not the package globals) so it does not
// mutate process-wide goose state. The DB pool must be the read-write role.
func (m *Module) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("search: migrate: %w", err)
	}
	db := stdlib.OpenDBFromPool(m.cfg.DB)
	defer func() { _ = db.Close() }()
	prov, err := goose.NewProvider(goose.DialectPostgres, db, sub,
		goose.WithTableName(migrationsTableName),
		goose.WithLogger(goose.NopLogger()),
	)
	if err != nil {
		return fmt.Errorf("search: migrate: %w", err)
	}
	if _, err := prov.Up(ctx); err != nil {
		return fmt.Errorf("search: migrate: %w", err)
	}
	return nil
}
