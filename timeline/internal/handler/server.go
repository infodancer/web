// Package handler is timeline's HTTP layer: the read UI, htmx partials, SEO
// endpoints, and the admin-gated export. It is constructed by the top-level
// timeline package from a validated config and mounted by the host under a
// chosen base path. The package owns no login flow — identity arrives through a
// [timelineuser.UserResolver] supplied by the host. Chrome comes from
// infodancer/ui via the render package.
package handler

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/media"
	"github.com/infodancer/web/timeline/internal/render"
	"github.com/infodancer/web/timeline/store"
	"github.com/infodancer/web/timeline/timelineuser"
)

//go:embed all:templates
var templateFS embed.FS

//go:embed all:static
var staticFS embed.FS

// portraitCategory is the media bucket timeline stores person portraits under.
const portraitCategory = "timeline-portrait"

// Deps is everything the handler needs, built by the timeline package from its
// public Config.
type Deps struct {
	Store     *store.Store              // required
	Resolver  timelineuser.UserResolver // required
	Logger    *slog.Logger
	RequestID func(context.Context) string // extracts a correlation id for log lines; nil logs an empty id
	BasePath  string                       // mount prefix, e.g. "/timeline"
	BaseURL   string                       // public base URL for absolute SEO links
	SignInURL string
	SiteName  string
	HomeURL   string
	LogoURL   string
	Chrome    func(*http.Request) uiassets.DocumentData // per-request host chrome; nil uses the module default
	Blobs     media.BlobStore                           // optional; nil disables portrait serving
	Objects   *media.Store                              // optional; portrait metadata
}

// Server is the HTTP layer. Construct with [New]; mount [Server.Routes].
type Server struct {
	store     *store.Store
	resolver  timelineuser.UserResolver
	log       *slog.Logger
	requestID func(context.Context) string
	basePath  string
	baseURL   string
	signInURL string
	siteName  string
	homeURL   string
	logoURL   string
	chrome    func(*http.Request) uiassets.DocumentData
	blobs     media.BlobStore
	objects   *media.Store
	templates *render.Templates
	static    fs.FS
	mux       *http.ServeMux
}

// New validates deps and returns a wired Server.
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
	requestID := d.RequestID
	if requestID == nil {
		requestID = func(context.Context) string { return "" }
	}
	if d.BasePath == "" {
		d.BasePath = "/timeline"
	}

	tpls, err := render.New(templateFS, nil, uiassets.PartialsFS(), d.Logger)
	if err != nil {
		return nil, fmt.Errorf("handler: templates: %w", err)
	}
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("handler: static fs: %w", err)
	}

	s := &Server{
		store:     d.Store,
		resolver:  d.Resolver,
		log:       d.Logger,
		requestID: requestID,
		basePath:  d.BasePath,
		baseURL:   d.BaseURL,
		signInURL: d.SignInURL,
		siteName:  d.SiteName,
		homeURL:   d.HomeURL,
		logoURL:   d.LogoURL,
		chrome:    d.Chrome,
		blobs:     d.Blobs,
		objects:   d.Objects,
		templates: tpls,
		static:    withUIAssets(staticSub),
		mux:       http.NewServeMux(),
	}
	s.registerRoutes()
	return s, nil
}

// Routes returns the module's HTTP handler wrapped in the user-resolving
// middleware. Mount it at the configured BasePath with http.StripPrefix.
func (s *Server) Routes() http.Handler {
	return s.resolveUserMiddleware(s.mux)
}

// logger returns a request-scoped logger carrying the request's correlation id,
// so every line for one request shares a request_id across components. The id
// comes from the injected extractor (empty when the host sets nothing).
func (s *Server) logger(r *http.Request) *slog.Logger {
	return s.log.With("request_id", s.requestID(r.Context()))
}

func (s *Server) registerRoutes() {
	s.mux.Handle("GET /static/", http.StripPrefix("/static/",
		cacheControl("public, max-age=86400, immutable", http.FileServer(http.FS(s.static)))))

	s.mux.HandleFunc("GET /portrait/{hash}", s.handlePortrait)

	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /t/{slug}", s.handleTimeline)
	s.mux.HandleFunc("GET /t/{slug}/event/{eventSlug}", s.handleEventDetail)
	s.mux.HandleFunc("GET /t/{slug}/people", s.handlePeopleList)
	s.mux.HandleFunc("GET /t/{slug}/person/{personSlug}", s.handlePersonProfile)

	s.mux.HandleFunc("GET /t/{slug}/sitemap.xml", s.handleSitemap)
	s.mux.HandleFunc("GET /t/{slug}/feed.atom", s.handleFeed)

	s.mux.HandleFunc("GET /t/{slug}/events", s.handleEventRange)
	s.mux.HandleFunc("GET /t/{slug}/event/{eventSlug}/inline", s.handleEventInline)
	s.mux.HandleFunc("GET /t/{slug}/search", s.handleSearch)
	s.mux.HandleFunc("GET /t/{slug}/density", s.handleDensity)

	s.mux.HandleFunc("GET /t/{slug}/export", s.requireAdmin(s.handleExport))
}

// handlePortrait streams a stored portrait by content hash from the media blob
// store. Returns 404 when no blob store is configured or the hash is unknown.
func (s *Server) handlePortrait(w http.ResponseWriter, r *http.Request) {
	if s.blobs == nil {
		http.NotFound(w, r)
		return
	}
	hash := r.PathValue("hash")
	data, contentType, err := s.blobs.Get(r.Context(), portraitCategory, hash)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	_, _ = w.Write(data)
}

// renderPage renders a full page (templates/pages/<page>) wrapped in the base
// view, which supplies ui chrome. embed=true is handled inside base.gohtml.
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, page string, data any) {
	if r.URL.Query().Get("embed") == "true" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	s.templates.Render(w, "pages/"+page, s.baseView(r, data))
}

// renderPartial renders an htmx fragment (templates/partials/<name>) directly,
// without chrome.
func (s *Server) renderPartial(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Cache-Control", "public, max-age=30")
	s.templates.Render(w, "partials/"+name, data)
}

// cacheControl wraps an http.Handler with a Cache-Control header.
func cacheControl(value string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		h.ServeHTTP(w, r)
	})
}

// withUIAssets serves infodancer/ui's assets under the "ui/" prefix in front of
// timeline's own embedded static files, so base.gohtml can link the shared CSS
// (and htmx) at {BasePath}/static/ui/... without the host mounting ui itself.
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
