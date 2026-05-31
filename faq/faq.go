// Package faq is a reusable Go module for database-driven FAQ / Q&A sections.
//
// The module serves imported and locally authored content under a custom htmx
// UI, and gives the host application a small surface to mount: New, Routes,
// Migrate. Imported content carries source attribution (domain, license,
// canonical URL) rendered alongside each post.
//
// See DESIGN.md at the repository root for architecture and conventions.
package faq

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/internal/handler"
	"github.com/infodancer/web/faq/internal/render"
	"github.com/infodancer/web/faq/store"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed all:templates
var templatesFS embed.FS

//go:embed all:static
var staticFS embed.FS

// Module is the runtime entry point. Construct one with [New] and mount its
// router with [Module.Routes]. A Module is safe for concurrent use.
type Module struct {
	cfg    Config
	server *handler.Server
}

// New constructs a Module from cfg. It returns an error if required fields
// (DB, Resolver) are missing. If cfg.AutoMigrate is true, New also runs the
// embedded migrations before returning.
func New(cfg Config) (*Module, error) {
	if cfg.DB == nil {
		return nil, errors.New("faq: Config.DB is required")
	}
	if cfg.Resolver == nil {
		return nil, errors.New("faq: Config.Resolver is required")
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
		return nil, fmt.Errorf("faq: templates: %w", err)
	}

	staticFiles, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("faq: static fs: %w", err)
	}

	srv, err := handler.New(handler.Deps{
		Store:        store.NewStore(cfg.DB),
		Templates:    tpls,
		Resolver:     cfg.Resolver,
		Logger:       cfg.Logger,
		BasePath:     cfg.BasePath,
		SiteName:     cfg.SiteName,
		HomeURL:      cfg.HomeURL,
		LogoURL:      cfg.LogoURL,
		SignInURL:    cfg.SignInURL,
		Chrome:       cfg.Chrome,
		PageSize:     cfg.PageSize,
		Static:       overlayStatic(withUIAssets(staticFiles), cfg.Static),
		CSRFDisabled: cfg.CSRF.Disable,
	})
	if err != nil {
		return nil, fmt.Errorf("faq: handler: %w", err)
	}
	m.server = srv
	return m, nil
}

// Routes returns the module's HTTP router, intended to be mounted at the
// configured BasePath in the host application's mux (with
// http.StripPrefix so the module sees paths relative to its mount point).
func (m *Module) Routes() http.Handler {
	return m.server.Routes()
}

// overlayStatic returns a [fs.FS] that prefers files in override and
// falls back to the embedded defaults. If override is nil the embedded
// FS is returned unchanged.
func overlayStatic(defaults, override fs.FS) fs.FS {
	if override == nil {
		return defaults
	}
	return overlayFS{primary: override, fallback: defaults}
}

// overlayFS implements fs.FS by trying primary first, then fallback.
// Used for the static-asset overlay where the host site may want to
// replace a single CSS file without forking the whole set.
type overlayFS struct {
	primary, fallback fs.FS
}

func (o overlayFS) Open(name string) (fs.File, error) {
	if f, err := o.primary.Open(name); err == nil {
		return f, nil
	}
	return o.fallback.Open(name)
}

// withUIAssets returns an [fs.FS] that serves infodancer/ui's assets at
// the "ui/" path prefix in front of faq's own embedded static files.
// Requests for /static/ui/css/tokens.css resolve to infodancer/ui's
// canonical token sheet; everything else stays in faq's own static.
//
// This makes the module self-contained — base.gohtml's <link> tags can
// reference the shared CSS without the host having to mount infodancer/ui
// separately.
func withUIAssets(faqStatic fs.FS) fs.FS {
	return uiRoutedFS{faq: faqStatic, ui: uiassets.AssetsFS()}
}

type uiRoutedFS struct {
	faq, ui fs.FS
}

func (u uiRoutedFS) Open(name string) (fs.File, error) {
	if name == "ui" || strings.HasPrefix(name, "ui/") {
		return u.ui.Open(strings.TrimPrefix(strings.TrimPrefix(name, "ui"), "/"))
	}
	return u.faq.Open(name)
}

// RecomputeCanonicalSubsites refreshes every question's canonical_subsite_id
// per the D8.3 policy: native (subsite_id IS NOT NULL) → owning subsite,
// import with exactly one matching subsite via tag rules → that subsite,
// import with zero or multiple matches → NULL (global is canonical).
//
// Idempotent and cheap on no-op runs (only rows whose canonical actually
// changes get written). Call after seeding or editing subsites, and after
// an import landed new questions.
func (m *Module) RecomputeCanonicalSubsites(ctx context.Context) error {
	if err := store.NewStore(m.cfg.DB).RecomputeCanonicalSubsites(ctx); err != nil {
		return fmt.Errorf("faq: recompute canonical subsites: %w", err)
	}
	return nil
}

// migrationsTableName is the dedicated goose version table for the faq
// module. Distinct from the default `goose_db_version` so faq's migration
// history does not collide with the host application's own goose tree when
// the two share a database.
const migrationsTableName = "faq_goose_db_version"

// Migrate runs the embedded goose migrations against the configured DB.
// Safe to call repeatedly; goose tracks applied versions in the dedicated
// faq_goose_db_version table.
//
// Uses goose.Provider (not the package-level globals) so the call does not
// mutate process-wide goose state shared with the host application.
func (m *Module) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("faq: migrate: %w", err)
	}
	db := stdlib.OpenDBFromPool(m.cfg.DB)
	defer func() { _ = db.Close() }()
	prov, err := goose.NewProvider(goose.DialectPostgres, db, sub,
		goose.WithTableName(migrationsTableName),
		goose.WithLogger(goose.NopLogger()),
	)
	if err != nil {
		return fmt.Errorf("faq: migrate: %w", err)
	}
	if _, err := prov.Up(ctx); err != nil {
		return fmt.Errorf("faq: migrate: %w", err)
	}
	return nil
}
