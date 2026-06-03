package web

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/infodancer/web/timeline/db"
)

//go:embed all:templates
var templateFS embed.FS

//go:embed all:static
var staticFS embed.FS

// Config holds the application configuration.
type Config struct {
	Port        string
	DatabaseURL string
	BaseURL     string
	ImageDir    string
}

// ConfigFromEnv reads application configuration from environment variables.
func ConfigFromEnv() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	imageDir := os.Getenv("IMAGE_DIR")
	if imageDir == "" {
		imageDir = "./data/images"
	}
	return &Config{
		Port:        port,
		DatabaseURL: os.Getenv("DATABASE_URL"),
		BaseURL:     os.Getenv("BASE_URL"),
		ImageDir:    imageDir,
	}
}

// Server is the main application server.
type Server struct {
	config    *Config
	templates map[string]*template.Template
	mux       *http.ServeMux
	store     *PgStore
	cue       *cueSchemas
}

// NewServer creates a new Server with database connection and templates.
func NewServer(ctx context.Context, config *Config) (*Server, error) {
	store, err := NewPgStore(ctx, config.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("new store: %w", err)
	}

	s := &Server{
		config: config,
		mux:    http.NewServeMux(),
		store:  store,
	}

	if err := s.loadTemplates(); err != nil {
		store.Close()
		return nil, fmt.Errorf("load templates: %w", err)
	}

	cueSchemas, err := loadCueSchemas()
	if err != nil {
		log.Printf("warning: CUE schemas not loaded: %v", err)
	}
	s.cue = cueSchemas

	s.routes()
	s.apiRoutes()
	return s, nil
}

// Close shuts down the server's resources.
func (s *Server) Close() {
	s.store.Close()
}

// Handler returns the server's HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Queries returns the sqlc queries for external use (e.g. sync).
func (s *Server) Queries() *db.Queries {
	return s.store.queries
}

func (s *Server) loadTemplates() error {
	baseURL := s.config.BaseURL
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
		// Normal version (with site header)
		t, err := template.New("").Funcs(funcMap).ParseFS(templateFS,
			"templates/base.html",
			"templates/"+page,
		)
		if err != nil {
			return fmt.Errorf("parse %s: %w", page, err)
		}
		s.templates[page] = t

		// Embed version (minimal chrome)
		te, err := template.New("").Funcs(funcMap).ParseFS(templateFS,
			"templates/embed_base.html",
			"templates/"+page,
		)
		if err != nil {
			return fmt.Errorf("parse embed %s: %w", page, err)
		}
		s.templates["embed:"+page] = te
	}

	// Partials (no base layout)
	partials := []string{
		"event_cards.html",
		"search_results.html",
		"event_inline.html",
	}
	for _, partial := range partials {
		t, err := template.New("").Funcs(funcMap).ParseFS(templateFS,
			"templates/"+partial,
		)
		if err != nil {
			return fmt.Errorf("parse %s: %w", partial, err)
		}
		s.templates[partial] = t
	}

	return nil
}

func (s *Server) routes() {
	// Static files — long cache, content is embedded and only changes on deploy
	staticSub, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/",
		cacheControl("public, max-age=86400, immutable", http.FileServer(http.FS(staticSub)))))

	// User-uploaded images — served from disk, moderate cache
	s.mux.Handle("GET /images/", http.StripPrefix("/images/",
		cacheControl("public, max-age=3600", http.FileServer(http.Dir(s.config.ImageDir)))))

	// Health check
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	// Pages
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /t/{slug}", s.handleTimeline)
	s.mux.HandleFunc("GET /t/{slug}/event/{eventSlug}", s.handleEventDetail)
	s.mux.HandleFunc("GET /t/{slug}/people", s.handlePeopleList)
	s.mux.HandleFunc("GET /t/{slug}/person/{personSlug}", s.handlePersonProfile)

	// SEO
	s.mux.HandleFunc("GET /t/{slug}/sitemap.xml", s.handleSitemap)
	s.mux.HandleFunc("GET /t/{slug}/feed.atom", s.handleFeed)

	// htmx partials
	s.mux.HandleFunc("GET /t/{slug}/events", s.handleEventRange)
	s.mux.HandleFunc("GET /t/{slug}/event/{eventSlug}/inline", s.handleEventInline)
	s.mux.HandleFunc("GET /t/{slug}/search", s.handleSearch)
	s.mux.HandleFunc("GET /t/{slug}/density", s.handleDensity)
	s.mux.HandleFunc("GET /t/{slug}/export", s.handleExport)
	s.mux.HandleFunc("POST /t/{slug}/import", s.handleImport)
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
