// Package blog is a reusable, mountable component for a site's
// authoritative editorial voice: a database-backed, multi-author blog with
// named editorial streams, a markdown authoring UX, the full feed matrix
// (RSS 2.0 / Atom / JSON Feed at combined, per-author, and per-blog
// scope with autodiscovery), JSON-LD, and a sitemap.
//
// The host supplies a database pool and a [bloguser.Resolver]; authorship
// is invitation-only via infodancer/authz role grants (see the blogauthz
// package). Author display identity, inline image storage, and search
// indexing reuse the suite's profile, media, and search components through
// host-injected hooks — blog owns none of them.
//
// The common surface a host mounts is [New], [Module.Routes], and
// [Module.Migrate], mirroring the other infodancer/web components.
package blog

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/infodancer/web/blog/internal/handler"
	"github.com/infodancer/web/blog/store"
	"github.com/infodancer/web/media"
)

//go:embed db/migrations/*.sql
var migrationsFS embed.FS

// migrationsTableName is blog's dedicated goose version table, distinct
// from the default so its migration history never collides with other
// components sharing the database.
const migrationsTableName = "blog_goose_db_version"

// ErrNotFound is re-exported from [store.ErrNotFound] for hosts branching
// on store lookups.
var ErrNotFound = store.ErrNotFound

// Module is the runtime entry point. Construct with [New]; mount
// [Module.Routes].
type Module struct {
	cfg    Config
	store  *store.Store
	server *handler.Server
}

// New constructs a Module from cfg. It returns an error when a required
// field (DB, Resolver) is missing. If cfg.AutoMigrate is set, New runs the
// embedded migrations before returning.
func New(cfg Config) (*Module, error) {
	if cfg.DB == nil {
		return nil, errors.New("blog: Config.DB is required")
	}
	if cfg.Resolver == nil {
		return nil, errors.New("blog: Config.Resolver is required")
	}
	cfg.applyDefaults()

	m := &Module{cfg: cfg, store: store.New(cfg.DB)}
	if cfg.AutoMigrate {
		if err := m.Migrate(context.Background()); err != nil {
			return nil, err
		}
	}

	srv, err := handler.New(handler.Deps{
		Store:           m.store,
		Resolver:        cfg.Resolver,
		Logger:          cfg.Logger,
		BasePath:        cfg.BasePath,
		BaseURL:         cfg.BaseURL,
		SiteName:        cfg.SiteName,
		SiteDescription: cfg.SiteDescription,
		HomeURL:         cfg.HomeURL,
		SignInURL:       cfg.SignInURL,
		Chrome:          cfg.Chrome,
		Authors:         cfg.Authors,
		AuthorCard:      cfg.AuthorCard,
		Indexer:         cfg.Indexer,
		IngestImage:     imageIngester(cfg),
		PageSize:        cfg.PageSize,
		FeedSize:        cfg.FeedSize,
		CSRFDisabled:    cfg.CSRF.Disable,
	})
	if err != nil {
		return nil, fmt.Errorf("blog: %w", err)
	}
	m.server = srv
	return m, nil
}

// imageIngester adapts media.Ingest to the handler's narrow upload hook.
// Returns nil (uploads disabled) unless both media stores are configured.
func imageIngester(cfg Config) handler.IngestImage {
	if cfg.Blobs == nil || cfg.Objects == nil {
		return nil
	}
	return func(ctx context.Context, subject string, r io.Reader) (string, error) {
		res, err := media.Ingest(ctx, cfg.Objects, cfg.Blobs, cfg.Media, subject, r)
		if err != nil {
			return "", err
		}
		return res.URL, nil
	}
}

// Routes returns the module's HTTP handler, intended to be mounted at
// cfg.BasePath in the host's mux with http.StripPrefix so the module sees
// paths relative to its mount point.
func (m *Module) Routes() http.Handler { return m.server.Routes() }

// Migrate applies the embedded migrations against the configured pool.
// Safe to call repeatedly; goose tracks applied versions in
// blog_goose_db_version.
func (m *Module) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationsFS, "db/migrations")
	if err != nil {
		return fmt.Errorf("blog: migrate: %w", err)
	}
	sqldb := stdlib.OpenDBFromPool(m.cfg.DB)
	defer func() { _ = sqldb.Close() }()
	prov, err := goose.NewProvider(goose.DialectPostgres, sqldb, sub,
		goose.WithTableName(migrationsTableName))
	if err != nil {
		return fmt.Errorf("blog: migrate: %w", err)
	}
	if _, err := prov.Up(ctx); err != nil {
		return fmt.Errorf("blog: migrate: %w", err)
	}
	return nil
}

// ReindexAll walks every published post into the configured search
// indexer, for boot-time reconciliation by the host. A nil Indexer makes
// it a no-op.
func (m *Module) ReindexAll(ctx context.Context) error {
	return m.server.ReindexAll(ctx)
}

// Store exposes the data layer for host-side needs beyond the HTTP
// surface (e.g. seeding named blogs or importing posts during a migration
// from a bespoke schema).
func (m *Module) Store() *store.Store { return m.store }
