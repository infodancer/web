// Package handler is profile's HTTP layer: the edit form, the public profile
// page, avatar upload, the identicon fallback, and the embeddable card. It is
// constructed by the top-level profile package from a validated config and
// mounted by the host under a chosen base path. The package owns no login
// flow -- identity arrives through a [profileuser.Resolver] supplied by the
// host. Chrome comes from infodancer/ui via the render package.
package handler

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	uiassets "github.com/infodancer/ui"
	"github.com/infodancer/ui/mdedit"

	"github.com/infodancer/web/profile/internal/render"
	"github.com/infodancer/web/profile/profileuser"
	"github.com/infodancer/web/profile/store"
)

//go:embed all:templates
var templateFS embed.FS

//go:embed all:static
var staticFS embed.FS

// ProfileStore is the slice of the data layer the handlers need. The concrete
// *store.Store satisfies it; tests use an in-memory fake.
type ProfileStore interface {
	Get(ctx context.Context, userKey string) (*store.Profile, error)
	GetByHandle(ctx context.Context, handle string) (*store.Profile, error)
	Upsert(ctx context.Context, p store.UpsertParams) (*store.Profile, error)
	ClaimHandle(ctx context.Context, userKey, handle string) error
	SetHandle(ctx context.Context, userKey, handle string) error
	SetAvatarURL(ctx context.Context, userKey, url string) error
}

// IngestAvatar stores one uploaded avatar image for subject (the user key)
// and returns its canonical public URL. The profile package wires this to
// media.Ingest; tests substitute a fake. nil disables uploads.
type IngestAvatar func(ctx context.Context, subject string, r io.Reader) (string, error)

// Section is one host-owned block profile places on its pages without ever
// parsing, validating, or storing its contents (DESIGN.md §3). The host
// renders the markup itself; an empty ViewHTML or EditHTML omits the section
// from that page.
type Section struct {
	Title    string
	ViewHTML template.HTML
	EditHTML template.HTML
}

// SectionsFunc supplies host-owned sections for one profile, called per
// request with the profile owner's user key. The host gates per-viewer
// visibility itself. nil means no host sections.
type SectionsFunc func(ctx context.Context, r *http.Request, userKey string) []Section

// Deps is everything the handler needs, built by the profile package from
// its public Config.
type Deps struct {
	Store        ProfileStore         // required
	Resolver     profileuser.Resolver // required
	Logger       *slog.Logger
	BasePath     string // mount prefix, e.g. "/profile"
	BaseURL      string // absolute site origin for canonical/JSON-LD; "" skips them
	SignInURL    string
	SiteName     string
	HomeURL      string
	LogoURL      string
	Chrome       func(*http.Request) uiassets.DocumentData // per-request host chrome; nil uses the module default
	Sections     SectionsFunc                              // host-owned page sections; nil means none
	OnChange     func(ctx context.Context, userKey string) // fired once per successful write; nil means none
	IngestAvatar IngestAvatar                              // nil disables avatar upload
	Reserved     []string                                  // host additions to the reserved-handle set
	CSRFDisabled bool
}

// Server is the HTTP layer. Construct with [New]; mount [Server.Routes].
type Server struct {
	store        ProfileStore
	resolver     profileuser.Resolver
	log          *slog.Logger
	basePath     string
	baseURL      string
	signInURL    string
	siteName     string
	homeURL      string
	logoURL      string
	chrome       func(*http.Request) uiassets.DocumentData
	sections     SectionsFunc
	onChange     func(ctx context.Context, userKey string)
	ingestAvatar IngestAvatar
	reserved     map[string]bool
	csrfDisabled bool
	templates    *render.Templates
	static       fs.FS
	mux          *http.ServeMux
}

// New validates deps and builds the Server.
func New(d Deps) (*Server, error) {
	if d.Store == nil {
		return nil, errors.New("handler: Deps.Store is required")
	}
	if d.Resolver == nil {
		return nil, errors.New("handler: Deps.Resolver is required")
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.BasePath == "" {
		d.BasePath = "/profile"
	}

	tpls, err := render.New(templateFS, uiassets.PartialsFS(), mdedit.PartialsFS(), d.Logger)
	if err != nil {
		return nil, err
	}
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("handler: static fs: %w", err)
	}

	reserved := make(map[string]bool, len(builtinReserved)+len(d.Reserved))
	for _, h := range builtinReserved {
		reserved[h] = true
	}
	for _, h := range d.Reserved {
		reserved[normalizeHandle(h)] = true
	}

	s := &Server{
		store:        d.Store,
		resolver:     d.Resolver,
		log:          d.Logger,
		basePath:     strings.TrimSuffix(d.BasePath, "/"),
		baseURL:      strings.TrimSuffix(d.BaseURL, "/"),
		signInURL:    d.SignInURL,
		siteName:     d.SiteName,
		homeURL:      d.HomeURL,
		logoURL:      d.LogoURL,
		chrome:       d.Chrome,
		sections:     d.Sections,
		onChange:     d.OnChange,
		ingestAvatar: d.IngestAvatar,
		reserved:     reserved,
		csrfDisabled: d.CSRFDisabled,
		templates:    tpls,
		static:       withUIAssets(staticSub),
		mux:          http.NewServeMux(),
	}
	s.registerRoutes()
	return s, nil
}

// Routes returns the module's HTTP handler wrapped in the user-resolving and
// CSRF middleware. Mount it at the configured BasePath with http.StripPrefix.
func (s *Server) Routes() http.Handler {
	return s.resolveUserMiddleware(s.csrfGuard(s.mux))
}

func (s *Server) registerRoutes() {
	s.mux.Handle("GET /static/", http.StripPrefix("/static/",
		cacheControl("public, max-age=86400, immutable", http.FileServer(http.FS(s.static)))))

	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /edit", s.requireUser(s.handleEditForm))
	s.mux.HandleFunc("POST /edit", s.requireUser(s.handleEditSave))
	s.mux.HandleFunc("POST /avatar", s.requireUser(s.handleAvatarUpload))
	s.mux.HandleFunc("GET /avatar/{handle}", s.handleIdenticon)

	// Literal routes above win over the handle wildcard; the reserved-handle
	// set additionally guarantees no claimable handle collides with a route.
	s.mux.HandleFunc("GET /{handle}", s.handleView)
}

// notifyChange fires the host's OnChange hook, if any. Call exactly once per
// request, after every write in the request has succeeded.
func (s *Server) notifyChange(ctx context.Context, userKey string) {
	if s.onChange != nil {
		s.onChange(ctx, userKey)
	}
}

// handleIndex sends signed-in users to their edit form; anonymous visitors
// have nothing to see at the bare mount point.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if profileuser.UserFromContext(r.Context()) == nil {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, s.basePath+"/edit", http.StatusSeeOther)
}

// renderPageWith renders a full page (templates/pages/<page>) wrapped in the
// base view, which supplies ui chrome. adjust (nilable) tweaks the assembled
// BaseView (per-page meta, head additions) before execution.
func (s *Server) renderPageWith(w http.ResponseWriter, r *http.Request, page string, status int, data any, adjust func(*BaseView)) {
	view := s.baseView(r, data)
	if adjust != nil {
		adjust(&view)
	}
	out, err := s.templates.Execute("pages/"+page, view)
	if err != nil {
		s.log.Error("render page", "page", page, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(out); err != nil {
		s.log.Warn("render: write", "page", page, "err", err)
	}
}

// cacheControl wraps an http.Handler with a Cache-Control header.
func cacheControl(value string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		h.ServeHTTP(w, r)
	})
}

// withUIAssets serves infodancer/ui's assets under the "ui/" prefix and
// mdedit's under "mdedit/", in front of profile's own embedded static files,
// so base.gohtml can link the shared CSS at {BasePath}/static/ui/... and the
// edit form can load the editor without the host mounting either itself.
func withUIAssets(own fs.FS) fs.FS {
	return uiRoutedFS{own: own, ui: uiassets.AssetsFS(), mdedit: mdedit.AssetsFS()}
}

type uiRoutedFS struct {
	own, ui, mdedit fs.FS
}

func (u uiRoutedFS) Open(name string) (fs.File, error) {
	if name == "ui" || strings.HasPrefix(name, "ui/") {
		return u.ui.Open(strings.TrimPrefix(strings.TrimPrefix(name, "ui"), "/"))
	}
	if name == "mdedit" || strings.HasPrefix(name, "mdedit/") {
		return u.mdedit.Open(strings.TrimPrefix(strings.TrimPrefix(name, "mdedit"), "/"))
	}
	return u.own.Open(name)
}
