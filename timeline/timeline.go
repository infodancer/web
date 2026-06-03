// Package timeline is a reusable, mountable component for interactive event
// timelines over extended timeframes. The host supplies a database pool and a
// [timelineuser.UserResolver]; the module serves an htmx UI, SEO endpoints, and
// an admin-gated export, and offers library methods to ingest data from a CUE
// directory tree ([Module.Sync]) or the JSON interchange format
// ([Module.Import]).
//
// The common surface a host mounts is [New], [Module.Routes], and
// [Module.Migrate], mirroring the other infodancer/web components.
package timeline

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/infodancer/web/timeline/db"
	"github.com/infodancer/web/timeline/internal/handler"
	"github.com/infodancer/web/timeline/internal/sync"
	"github.com/infodancer/web/timeline/internal/transfer"
	"github.com/infodancer/web/timeline/store"
)

//go:embed db/migrations/*.sql
var migrationsFS embed.FS

// migrationsTableName is timeline's dedicated goose version table, distinct
// from the default so its migration history never collides with other
// components sharing the database.
const migrationsTableName = "timeline_goose_db_version"

// Module is the runtime entry point. Construct with [New]; mount [Module.Routes].
type Module struct {
	cfg    Config
	store  *store.Store
	server *handler.Server
}

// New constructs a Module from cfg. It returns an error when a required field
// (DB, Resolver) is missing. If cfg.AutoMigrate is set, New runs the embedded
// migrations before returning.
func New(cfg Config) (*Module, error) {
	if cfg.DB == nil {
		return nil, errors.New("timeline: Config.DB is required")
	}
	if cfg.Resolver == nil {
		return nil, errors.New("timeline: Config.Resolver is required")
	}
	cfg.applyDefaults()

	m := &Module{cfg: cfg, store: store.NewStore(cfg.DB)}
	if cfg.AutoMigrate {
		if err := m.Migrate(context.Background()); err != nil {
			return nil, err
		}
	}

	srv, err := handler.New(handler.Deps{
		Store:     m.store,
		Resolver:  cfg.Resolver,
		Logger:    cfg.Logger,
		BasePath:  cfg.BasePath,
		BaseURL:   cfg.BaseURL,
		SignInURL: cfg.SignInURL,
		Blobs:     cfg.Blobs,
		Objects:   cfg.Objects,
	})
	if err != nil {
		return nil, fmt.Errorf("timeline: %w", err)
	}
	m.server = srv
	return m, nil
}

// Routes returns the module's HTTP handler, intended to be mounted at
// cfg.BasePath in the host's mux with http.StripPrefix so the module sees
// paths relative to its mount point.
func (m *Module) Routes() http.Handler { return m.server.Routes() }

// Migrate applies the embedded migrations against the configured pool. Safe to
// call repeatedly; goose tracks applied versions in timeline_goose_db_version.
func (m *Module) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationsFS, "db/migrations")
	if err != nil {
		return fmt.Errorf("timeline: migrate: %w", err)
	}
	sqldb := stdlib.OpenDBFromPool(m.cfg.DB)
	defer func() { _ = sqldb.Close() }()
	prov, err := goose.NewProvider(goose.DialectPostgres, sqldb, sub,
		goose.WithTableName(migrationsTableName),
		goose.WithLogger(goose.NopLogger()),
	)
	if err != nil {
		return fmt.Errorf("timeline: migrate: %w", err)
	}
	if _, err := prov.Up(ctx); err != nil {
		return fmt.Errorf("timeline: migrate: %w", err)
	}
	return nil
}

// Sync ingests a CUE directory tree into the database, creating or updating the
// timeline it describes. Sibling portrait images are stored through the
// configured media blob store. See the [sync] package for the directory layout.
func (m *Module) Sync(ctx context.Context, dir string) (sync.Stats, error) {
	runner := sync.New(m.store.Queries, m.cfg.Blobs, m.cfg.Objects, m.cfg.BasePath)
	return runner.Run(ctx, dir)
}

// Import materializes a JSON interchange document into the timeline named by
// slug (creating it if absent), atomically inside a transaction.
func (m *Module) Import(ctx context.Context, slug string, data transfer.TimelineExport) (transfer.Stats, error) {
	var stats transfer.Stats
	err := m.store.WithTx(ctx, func(q *db.Queries) error {
		var err error
		stats, err = transfer.Import(ctx, q, slug, data)
		return err
	})
	return stats, err
}
