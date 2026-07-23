// Package handler is the HTTP layer of the search module: a search page shell,
// an HTMX results partial, static assets, and a health check. Mount it via
// [Server.Routes] under the module's BasePath. Modeled on faq/internal/handler.
package handler

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/infodancer/ui"

	"github.com/infodancer/web/search/internal/query"
	"github.com/infodancer/web/search/internal/render"
)

// Searcher is the read-path dependency the handler calls. *query.Engine
// satisfies it; tests supply a fake.
type Searcher interface {
	Search(ctx context.Context, q string, limit int) ([]query.Result, error)
}

// Deps captures everything the handler needs. The search package builds this
// from the public Config when it constructs the module.
type Deps struct {
	// Engine runs queries. Required.
	Engine Searcher

	// Templates is the parsed template set. Required.
	Templates *render.Templates

	// Logger is the per-server slog handle. Defaults to slog.Default.
	Logger *slog.Logger

	// BasePath is the URL prefix the module is mounted at (e.g. "/search").
	BasePath string

	// SiteName, HomeURL, LogoURL drive the default nav/footer chrome (used when
	// Chrome is nil).
	SiteName string
	HomeURL  string
	LogoURL  string

	// Chrome, when set, supplies the full host page chrome per request as a
	// ui.DocumentData rendered through ui/document. nil falls back to the
	// SiteName/HomeURL-derived default (see newPageView).
	Chrome func(*http.Request) ui.DocumentData

	// PageSize is how many results a query returns. Clamped to [1,100].
	PageSize int

	// Static is the filesystem for bundled assets (search.css + the ui/ CSS
	// overlay). Optional in tests; set by the module on the New path.
	Static fs.FS

	// CSRFDisabled short-circuits the same-origin guard when the host enforces
	// an equivalent one at a higher layer.
	CSRFDisabled bool
}

// Server is the HTTP layer. Construct with [New]; mount with [Server.Routes].
type Server struct {
	deps         Deps
	mux          *http.ServeMux
	csrfDisabled bool
}

// New validates deps and returns a wired Server.
func New(d Deps) (*Server, error) {
	if d.Engine == nil {
		return nil, errors.New("handler: Deps.Engine is required")
	}
	if d.Templates == nil {
		return nil, errors.New("handler: Deps.Templates is required")
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.BasePath == "" {
		d.BasePath = "/search"
	}
	if d.HomeURL == "" {
		d.HomeURL = "/"
	}
	if d.SiteName == "" {
		d.SiteName = "Search"
	}
	switch {
	case d.PageSize <= 0:
		d.PageSize = 20
	case d.PageSize > 100:
		d.PageSize = 100
	}

	s := &Server{deps: d, mux: http.NewServeMux(), csrfDisabled: d.CSRFDisabled}
	s.registerRoutes()
	return s, nil
}

// Routes returns the handler wrapped in the body-size limit and same-origin
// CSRF guard. Mount it at BasePath with http.StripPrefix.
func (s *Server) Routes() http.Handler {
	return s.limitBody(s.csrfGuard(s.mux))
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /{$}", s.searchPage)    // page shell (also serves ?q= without JS)
	s.mux.HandleFunc("GET /search", s.searchFrag) // HTMX results partial
	s.mux.HandleFunc("GET /healthz", s.healthz)

	if s.deps.Static != nil {
		s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(s.deps.Static)))
	}

	s.mux.HandleFunc("/", s.notFound)
}

// maxBodyBytes caps request bodies. Search is read-only (GET), so this only
// matters if a host later mounts a state-changing endpoint; applying it
// uniformly costs nothing on GET.
const maxBodyBytes = 64 * 1024

func (s *Server) limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// isHTMX reports whether the request was issued by htmx.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}
