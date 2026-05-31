// Package admin is the mountable admin UI for the notify module: a notification
// bell and an event feed, rendered with HTMX on top of infodancer/ui.
//
// The package owns no authentication. The host application gates access (mount
// the router behind its own admin RBAC) and supplies a [Resolver] that extracts
// the current admin identity from a request. Construct a [Handler] with [New]
// and mount [Handler.Routes] under [Config.BasePath] with http.StripPrefix.
package admin

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/notify"
)

//go:embed all:templates
var templatesFS embed.FS

//go:embed all:static
var staticFS embed.FS

// Store is the read/cursor surface the admin UI needs. *notify.Store satisfies
// it; tests can supply a fake.
type Store interface {
	AdminFeed(ctx context.Context, f notify.Filter) ([]notify.Event, error)
	AdminUnreadCount(ctx context.Context, id notify.Identity) (int, error)
	AdminMarkSeen(ctx context.Context, id notify.Identity, upTo time.Time) error
}

// Resolver extracts the current admin identity from a request. Returning false
// denies access (the handler responds 403). The host is responsible for the
// actual authentication and role check; notify trusts this verdict.
type Resolver interface {
	Resolve(r *http.Request) (notify.Identity, bool)
}

// ResolverFunc adapts a function to a [Resolver].
type ResolverFunc func(r *http.Request) (notify.Identity, bool)

// Resolve calls f.
func (f ResolverFunc) Resolve(r *http.Request) (notify.Identity, bool) { return f(r) }

// Config captures the wiring the host supplies to [New]. Store and Resolver are
// required; everything else has a sensible default.
type Config struct {
	// Store reads events and the admin cursor. Required.
	Store Store

	// Resolver identifies (and authorizes) the current admin. Required.
	Resolver Resolver

	// Logger is used for module-level logging. Defaults to slog.Default.
	Logger *slog.Logger

	// BasePath is the URL prefix the router is mounted at, used for link and
	// asset generation. Defaults to "/admin/notifications".
	BasePath string

	// SiteName is the host's display name shown in the chrome. Defaults to
	// "Notifications".
	SiteName string

	// HomeURL is where the brand link points. Defaults to "/".
	HomeURL string

	// Chrome, when set, supplies the full host page chrome (nav, footer, meta,
	// theme, analytics, asset base, head tags) per request as a
	// [ui.DocumentData] that pages render through ui/document. A host site sets
	// this to inject its own live, auth-aware navigation so the admin UI matches
	// the rest of the site. nil keeps a SiteName/HomeURL-derived default chrome,
	// so a standalone mount still renders. Either way the notification bell is
	// the GET {BasePath}/bell fragment the host embeds in its own nav; notify's
	// own page does not render it.
	Chrome func(r *http.Request) uiassets.DocumentData

	// PageSize is the feed page size, clamped to [1, 100]. Defaults to 25.
	PageSize int

	// PollSeconds is how often the bell re-fetches its unread count via HTMX.
	// Defaults to 30.
	PollSeconds int

	// Templates and Static optionally overlay the embedded assets; same-named
	// files take precedence over the defaults.
	Templates fs.FS
	Static    fs.FS
}

func (c *Config) applyDefaults() {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.BasePath == "" {
		c.BasePath = "/admin/notifications"
	}
	c.BasePath = strings.TrimRight(c.BasePath, "/")
	if c.SiteName == "" {
		c.SiteName = "Notifications"
	}
	if c.HomeURL == "" {
		c.HomeURL = "/"
	}
	if c.PageSize <= 0 || c.PageSize > 100 {
		c.PageSize = 25
	}
	if c.PollSeconds <= 0 {
		c.PollSeconds = 30
	}
}

// Handler serves the admin notification UI. Construct it with [New].
type Handler struct {
	store    Store
	resolver Resolver
	tpl      *templates
	log      *slog.Logger
	static   http.Handler

	basePath string
	siteName string
	homeURL  string
	pageSize int

	// now is the clock used for relative timestamps; overridable in tests.
	now func() time.Time

	// chrome supplies the host page chrome per request; nil uses defaultChrome.
	chrome func(r *http.Request) uiassets.DocumentData
}

// New constructs a Handler from cfg. It returns an error if Store or Resolver
// is missing or the templates fail to parse.
func New(cfg Config) (*Handler, error) {
	if cfg.Store == nil {
		return nil, errors.New("admin: Config.Store is required")
	}
	if cfg.Resolver == nil {
		return nil, errors.New("admin: Config.Resolver is required")
	}
	cfg.applyDefaults()

	tpl, err := newTemplates(templatesFS, cfg.Templates, uiassets.PartialsFS(), cfg.Logger)
	if err != nil {
		return nil, err
	}

	staticFiles, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("admin: static fs: %w", err)
	}
	assets := overlayStatic(withUIAssets(staticFiles), cfg.Static)

	h := &Handler{
		store:    cfg.Store,
		resolver: cfg.Resolver,
		tpl:      tpl,
		log:      cfg.Logger,
		static:   http.StripPrefix("/static/", http.FileServerFS(assets)),
		basePath: cfg.BasePath,
		siteName: cfg.SiteName,
		homeURL:  cfg.HomeURL,
		pageSize: cfg.PageSize,
		now:      time.Now,
		chrome:   cfg.Chrome,
	}
	return h, nil
}

// Routes returns the module router. Mount it at BasePath in the host mux with
// http.StripPrefix so the module sees paths relative to its mount point.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.feed)
	mux.HandleFunc("GET /bell", h.bell)
	mux.HandleFunc("POST /seen", h.seen)
	mux.Handle("GET /static/", h.static)
	return mux
}

// overlayStatic prefers files in override, falling back to defaults.
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

// withUIAssets serves infodancer/ui's assets under the "ui/" prefix, in front
// of notify's own static files, so base.gohtml can reference the shared CSS and
// vendored htmx without the host mounting infodancer/ui separately.
func withUIAssets(notifyStatic fs.FS) fs.FS {
	return uiRoutedFS{notify: notifyStatic, ui: uiassets.AssetsFS()}
}

type uiRoutedFS struct {
	notify, ui fs.FS
}

func (u uiRoutedFS) Open(name string) (fs.File, error) {
	if name == "ui" || strings.HasPrefix(name, "ui/") {
		return u.ui.Open(strings.TrimPrefix(strings.TrimPrefix(name, "ui"), "/"))
	}
	return u.notify.Open(name)
}
