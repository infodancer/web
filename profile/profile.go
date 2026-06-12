// Package profile is a reusable, mountable component for the public persona
// every community site needs: a unique handle, a display name, a short
// markdown bio, an avatar, and bio links. The host supplies a database pool
// and a [profileuser.Resolver]; the module serves the edit form, the public
// profile page, avatar upload through web/media, a deterministic identicon
// fallback, and an embeddable card for other components (forum posts,
// rosters, bylines).
//
// The component deliberately does not own users, sessions, or
// authentication: profiles are keyed by an opaque, host-supplied user key,
// so the host's users table stays the host's FK hub. See
// https://github.com/infodancer/web/issues/52 for the design rationale.
//
// The common surface a host mounts is [New], [Module.Routes], and
// [Module.Migrate], mirroring the other infodancer/web components.
package profile

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/infodancer/web/media"
	"github.com/infodancer/web/profile/internal/handler"
	"github.com/infodancer/web/profile/store"
)

//go:embed db/migrations/*.sql
var migrationsFS embed.FS

// migrationsTableName is profile's dedicated goose version table, distinct
// from the default so its migration history never collides with other
// components sharing the database.
const migrationsTableName = "profile_goose_db_version"

// ErrNotFound is re-exported from [store.ErrNotFound] for hosts branching on
// [Module.Card] misses.
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
		return nil, errors.New("profile: Config.DB is required")
	}
	if cfg.Resolver == nil {
		return nil, errors.New("profile: Config.Resolver is required")
	}
	cfg.applyDefaults()

	m := &Module{cfg: cfg, store: store.New(cfg.DB)}
	if cfg.AutoMigrate {
		if err := m.Migrate(context.Background()); err != nil {
			return nil, err
		}
	}

	srv, err := handler.New(handler.Deps{
		Store:        m.store,
		Resolver:     cfg.Resolver,
		Logger:       cfg.Logger,
		BasePath:     cfg.BasePath,
		SignInURL:    cfg.SignInURL,
		SiteName:     cfg.SiteName,
		HomeURL:      cfg.HomeURL,
		LogoURL:      cfg.LogoURL,
		Chrome:       cfg.Chrome,
		Sections:     cfg.Sections,
		IngestAvatar: avatarIngester(cfg),
		Reserved:     cfg.ReservedHandles,
		CSRFDisabled: cfg.CSRF.Disable,
	})
	if err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	m.server = srv
	return m, nil
}

// avatarIngester adapts media.Ingest to the handler's narrow upload hook.
// Returns nil (uploads disabled) unless both media stores are configured.
func avatarIngester(cfg Config) handler.IngestAvatar {
	if cfg.Blobs == nil || cfg.Objects == nil {
		return nil
	}
	return func(ctx context.Context, subject string, r io.Reader) (string, error) {
		res, err := media.Ingest(ctx, cfg.Objects, cfg.Blobs, cfg.Avatar, subject, r)
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

// Migrate applies the embedded migrations against the configured pool. Safe
// to call repeatedly; goose tracks applied versions in
// profile_goose_db_version.
func (m *Module) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationsFS, "db/migrations")
	if err != nil {
		return fmt.Errorf("profile: migrate: %w", err)
	}
	sqldb := stdlib.OpenDBFromPool(m.cfg.DB)
	defer func() { _ = sqldb.Close() }()
	prov, err := goose.NewProvider(goose.DialectPostgres, sqldb, sub,
		goose.WithTableName(migrationsTableName))
	if err != nil {
		return fmt.Errorf("profile: migrate: %w", err)
	}
	if _, err := prov.Up(ctx); err != nil {
		return fmt.Errorf("profile: migrate: %w", err)
	}
	return nil
}

// Card renders the embeddable avatar+name unit for one user, for other
// components and host surfaces to inline next to user-authored content.
// Returns [ErrNotFound] when the user has no profile row.
func (m *Module) Card(ctx context.Context, userKey string) (template.HTML, error) {
	return m.server.Card(ctx, userKey)
}

// Store exposes the data layer for host-side needs beyond the HTTP surface
// (e.g. seeding profiles during a migration from a bespoke schema).
func (m *Module) Store() *store.Store { return m.store }
