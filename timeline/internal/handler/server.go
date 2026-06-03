// Package handler is timeline's HTTP layer: the read UI, htmx partials, SEO
// endpoints, and the admin-gated export. It is constructed by the top-level
// timeline package from a validated config and mounted by the host under a
// chosen base path. The package owns no login flow — identity arrives through a
// [timelineuser.UserResolver] supplied by the host.
package handler

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"time"

	"github.com/infodancer/web/media"
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
	BasePath  string // mount prefix, e.g. "/timeline"
	BaseURL   string // public base URL for absolute SEO links; empty falls back to the request Host
	SignInURL string
	Blobs     media.BlobStore // optional; nil disables portrait serving
	Objects   *media.Store    // optional; portrait metadata
}

// Server is the HTTP layer. Construct with [New]; mount [Server.Routes].
type Server struct {
	store     *store.Store
	resolver  timelineuser.UserResolver
	log       *slog.Logger
	basePath  string
	baseURL   string
	signInURL string
	blobs     media.BlobStore
	objects   *media.Store
	templates map[string]*template.Template
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
	if d.BasePath == "" {
		d.BasePath = "/timeline"
	}
	s := &Server{
		store:     d.Store,
		resolver:  d.Resolver,
		log:       d.Logger,
		basePath:  d.BasePath,
		baseURL:   d.BaseURL,
		signInURL: d.SignInURL,
		blobs:     d.Blobs,
		objects:   d.Objects,
		mux:       http.NewServeMux(),
	}
	if err := s.loadTemplates(); err != nil {
		return nil, fmt.Errorf("handler: %w", err)
	}
	s.registerRoutes()
	return s, nil
}

// Routes returns the module's HTTP handler wrapped in the user-resolving
// middleware. Mount it at the configured BasePath with http.StripPrefix so the
// module sees paths relative to its mount point.
func (s *Server) Routes() http.Handler {
	return s.resolveUserMiddleware(s.mux)
}

func (s *Server) registerRoutes() {
	staticSub, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/",
		cacheControl("public, max-age=86400, immutable", http.FileServer(http.FS(staticSub)))))

	// Portraits stream from the media blob store (content-addressed by hash).
	s.mux.HandleFunc("GET /portrait/{hash}", s.handlePortrait)

	// Pages.
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /t/{slug}", s.handleTimeline)
	s.mux.HandleFunc("GET /t/{slug}/event/{eventSlug}", s.handleEventDetail)
	s.mux.HandleFunc("GET /t/{slug}/people", s.handlePeopleList)
	s.mux.HandleFunc("GET /t/{slug}/person/{personSlug}", s.handlePersonProfile)

	// SEO.
	s.mux.HandleFunc("GET /t/{slug}/sitemap.xml", s.handleSitemap)
	s.mux.HandleFunc("GET /t/{slug}/feed.atom", s.handleFeed)

	// htmx partials.
	s.mux.HandleFunc("GET /t/{slug}/events", s.handleEventRange)
	s.mux.HandleFunc("GET /t/{slug}/event/{eventSlug}/inline", s.handleEventInline)
	s.mux.HandleFunc("GET /t/{slug}/search", s.handleSearch)
	s.mux.HandleFunc("GET /t/{slug}/density", s.handleDensity)

	// Admin-gated export.
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

func (s *Server) loadTemplates() error {
	baseURL := s.baseURL
	funcMap := template.FuncMap{
		"formatDate": formatDate,
		"safeHTML":   func(s string) template.HTML { return template.HTML(s) },
		"even":       func(i int) bool { return i%2 == 0 },
		"baseURL":    func() string { return baseURL },
	}

	s.templates = make(map[string]*template.Template)

	pages := []string{
		"index.html",
		"timeline.html",
		"event_detail.html",
		"person_profile.html",
		"people_list.html",
	}
	for _, page := range pages {
		t, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/base.html", "templates/"+page)
		if err != nil {
			return fmt.Errorf("parse %s: %w", page, err)
		}
		s.templates[page] = t

		te, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/embed_base.html", "templates/"+page)
		if err != nil {
			return fmt.Errorf("parse embed %s: %w", page, err)
		}
		s.templates["embed:"+page] = te
	}

	partials := []string{"event_cards.html", "search_results.html", "event_inline.html"}
	for _, partial := range partials {
		t, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/"+partial)
		if err != nil {
			return fmt.Errorf("parse %s: %w", partial, err)
		}
		s.templates[partial] = t
	}
	return nil
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data any) {
	key := name
	if r.URL.Query().Get("embed") == "true" {
		key = "embed:" + name
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	t, ok := s.templates[key]
	if !ok {
		log.Printf("template %q not found", key)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (s *Server) renderPartial(w http.ResponseWriter, name string, data any) {
	t, ok := s.templates[name]
	if !ok {
		log.Printf("template %q not found", name)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=30")
	if err := t.ExecuteTemplate(w, "content", data); err != nil {
		log.Printf("render partial %s: %v", name, err)
	}
}

// cacheControl wraps an http.Handler with a Cache-Control header.
func cacheControl(value string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		h.ServeHTTP(w, r)
	})
}

// formatDate renders a date according to precision.
func formatDate(t time.Time, precision string) string {
	switch precision {
	case "year":
		return t.Format("2006")
	case "month":
		return t.Format("January 2006")
	default:
		return t.Format("January 2, 2006")
	}
}
