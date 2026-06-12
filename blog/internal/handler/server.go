// Package handler is blog's HTTP layer: the public read surface (indexes,
// posts, authors, tags), the feed matrix, the sitemap, the authoring write
// path, and named-blog admin. It is constructed by the top-level blog
// package from a validated config and mounted by the host under a chosen
// base path. The package owns no login flow — identity arrives through a
// [bloguser.Resolver]; chrome comes from infodancer/ui via the render
// package.
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
	"time"

	uiassets "github.com/infodancer/ui"
	"github.com/infodancer/ui/mdedit"

	"github.com/infodancer/web/blog/bloguser"
	"github.com/infodancer/web/blog/internal/render"
	"github.com/infodancer/web/blog/store"
)

//go:embed all:templates
var templateFS embed.FS

//go:embed all:static
var staticFS embed.FS

// BlogStore is the slice of the data layer the handlers need. The concrete
// *store.Store satisfies it; tests use an in-memory fake.
type BlogStore interface {
	CreateBlog(ctx context.Context, slug, name, description string) (*store.Blog, error)
	UpdateBlog(ctx context.Context, id int64, slug, name, description string) (*store.Blog, error)
	DeleteBlog(ctx context.Context, id int64) error
	GetBlogBySlug(ctx context.Context, slug string) (*store.Blog, error)
	ListBlogs(ctx context.Context) ([]store.Blog, error)

	EnsureAuthor(ctx context.Context, userKey, wantSlug, displayName string) (*store.Author, error)
	GetAuthorBySlug(ctx context.Context, slug string) (*store.Author, error)
	ListAuthors(ctx context.Context) ([]store.Author, error)

	CreatePost(ctx context.Context, baseSlug, status string, p store.PostParams) (*store.Post, error)
	UpdatePost(ctx context.Context, id int64, newSlug string, p store.PostParams) (*store.Post, error)
	SetPostStatus(ctx context.Context, id int64, status string) (*store.Post, error)
	SoftDeletePost(ctx context.Context, id int64) error
	GetPostByID(ctx context.Context, id int64) (*store.Post, error)
	GetPostBySlug(ctx context.Context, slug string) (*store.Post, error)
	ListPublished(ctx context.Context, limit, offset int) ([]store.Post, int64, error)
	ListPublishedByBlog(ctx context.Context, blogSlug string, limit, offset int) ([]store.Post, int64, error)
	ListPublishedByAuthor(ctx context.Context, authorSlug string, limit, offset int) ([]store.Post, int64, error)
	ListPublishedByTag(ctx context.Context, tagSlug string, limit, offset int) ([]store.Post, int64, error)
	ListByAuthorKeyAnyStatus(ctx context.Context, authorKey string, limit, offset int) ([]store.Post, error)

	SetPostTags(ctx context.Context, postID int64, tags []store.TagInput) error
	GetTagBySlug(ctx context.Context, slug string) (*store.Tag, error)
	ListTags(ctx context.Context) ([]store.Tag, error)
	TagsForPosts(ctx context.Context, postIDs []int64) (map[int64][]store.Tag, error)
}

// AuthorInfo is the structured display identity the host's profile system
// knows about a user; see the top-level blog package for the directory
// contract.
type AuthorInfo struct {
	Slug        string
	DisplayName string
	AvatarURL   string
	BioHTML     template.HTML
}

// AuthorDirectory resolves a user key to profile-backed display identity.
// (nil, nil) means the host knows nothing extra about the user.
type AuthorDirectory interface {
	Lookup(ctx context.Context, userKey string) (*AuthorInfo, error)
}

// SearchDocument is one published post as the host's search index sees it.
type SearchDocument struct {
	ExternalID  string
	URL         string
	Title       string
	Summary     string
	Tags        []string
	BodyMD      string
	PublishedAt time.Time
}

// Indexer is the host-wired search hook. Implementations adapt
// infodancer/web/search's Module; errors are logged, never user-facing.
type Indexer interface {
	IndexPost(ctx context.Context, doc SearchDocument) error
	DeletePost(ctx context.Context, externalID string) error
}

// IngestImage stores one uploaded inline image for subject and returns its
// canonical public URL. The blog package wires this to media.Ingest; tests
// substitute a fake. nil disables uploads.
type IngestImage func(ctx context.Context, subject string, r io.Reader) (string, error)

// Deps is everything the handler needs, built by the blog package from its
// public Config.
type Deps struct {
	Store    BlogStore         // required
	Resolver bloguser.Resolver // required
	Logger   *slog.Logger

	BasePath        string // mount prefix, e.g. "/blog"
	BaseURL         string // absolute site origin for feeds/sitemap/JSON-LD; "" degrades to relative
	SiteName        string
	SiteDescription string
	HomeURL         string
	SignInURL       string
	Chrome          func(*http.Request) uiassets.DocumentData

	Authors     AuthorDirectory // nil: registry falls back to resolver display names
	AuthorCard  func(ctx context.Context, userKey string) (template.HTML, error)
	Indexer     Indexer     // nil disables search indexing
	IngestImage IngestImage // nil disables inline image upload

	PageSize     int
	FeedSize     int
	MaxBodyBytes int64 // form parse cap; defaulted by the blog package
	CSRFDisabled bool
}

// Server is the HTTP layer. Construct with [New]; mount [Server.Routes].
type Server struct {
	store        BlogStore
	resolver     bloguser.Resolver
	log          *slog.Logger
	basePath     string
	baseURL      string
	siteName     string
	siteDesc     string
	homeURL      string
	signInURL    string
	chrome       func(*http.Request) uiassets.DocumentData
	authors      AuthorDirectory
	authorCard   func(ctx context.Context, userKey string) (template.HTML, error)
	indexer      Indexer
	ingestImage  IngestImage
	pageSize     int
	feedSize     int
	maxBodyBytes int64
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
		d.BasePath = "/blog"
	}
	if d.SiteName == "" {
		d.SiteName = "Blog"
	}
	if d.PageSize <= 0 {
		d.PageSize = 20
	}
	if d.FeedSize <= 0 {
		d.FeedSize = 20
	}
	if d.MaxBodyBytes <= 0 {
		d.MaxBodyBytes = 1 << 20 // 1 MiB of form text
	}

	tpls, err := render.New(templateFS, uiassets.PartialsFS(), mdedit.PartialsFS(), d.Logger)
	if err != nil {
		return nil, err
	}
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("handler: static fs: %w", err)
	}

	s := &Server{
		store:        d.Store,
		resolver:     d.Resolver,
		log:          d.Logger,
		basePath:     strings.TrimSuffix(d.BasePath, "/"),
		baseURL:      strings.TrimSuffix(d.BaseURL, "/"),
		siteName:     d.SiteName,
		siteDesc:     d.SiteDescription,
		homeURL:      d.HomeURL,
		signInURL:    d.SignInURL,
		chrome:       d.Chrome,
		authors:      d.Authors,
		authorCard:   d.AuthorCard,
		indexer:      d.Indexer,
		ingestImage:  d.IngestImage,
		pageSize:     d.PageSize,
		feedSize:     d.FeedSize,
		maxBodyBytes: d.MaxBodyBytes,
		csrfDisabled: d.CSRFDisabled,
		templates:    tpls,
		static:       withSuiteAssets(staticSub),
		mux:          http.NewServeMux(),
	}
	s.registerRoutes()
	return s, nil
}

// Routes returns the module's HTTP handler wrapped in the user-resolving
// and CSRF middleware. Mount it at the configured BasePath with
// http.StripPrefix.
func (s *Server) Routes() http.Handler {
	return s.resolveUserMiddleware(s.csrfGuard(s.mux))
}

func (s *Server) registerRoutes() {
	s.mux.Handle("GET /static/", http.StripPrefix("/static/",
		cacheControl("public, max-age=86400, immutable", http.FileServer(http.FS(s.static)))))

	// Read surface.
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /p/{slug}", s.handlePost)
	s.mux.HandleFunc("GET /b/{slug}", s.handleBlogIndex)
	s.mux.HandleFunc("GET /authors", s.handleAuthors)
	s.mux.HandleFunc("GET /authors/{slug}", s.handleAuthor)
	s.mux.HandleFunc("GET /tags", s.handleTags)
	s.mux.HandleFunc("GET /tags/{slug}", s.handleTag)

	// Feeds: combined, per-author, per-named-blog × RSS/Atom/JSON.
	s.mux.HandleFunc("GET /feed.xml", s.feedHandler(feedScopeAll, "rss"))
	s.mux.HandleFunc("GET /feed.atom", s.feedHandler(feedScopeAll, "atom"))
	s.mux.HandleFunc("GET /feed.json", s.feedHandler(feedScopeAll, "json"))
	s.mux.HandleFunc("GET /authors/{slug}/feed.xml", s.feedHandler(feedScopeAuthor, "rss"))
	s.mux.HandleFunc("GET /authors/{slug}/feed.atom", s.feedHandler(feedScopeAuthor, "atom"))
	s.mux.HandleFunc("GET /authors/{slug}/feed.json", s.feedHandler(feedScopeAuthor, "json"))
	s.mux.HandleFunc("GET /b/{slug}/feed.xml", s.feedHandler(feedScopeBlog, "rss"))
	s.mux.HandleFunc("GET /b/{slug}/feed.atom", s.feedHandler(feedScopeBlog, "atom"))
	s.mux.HandleFunc("GET /b/{slug}/feed.json", s.feedHandler(feedScopeBlog, "json"))

	s.mux.HandleFunc("GET /sitemap.xml", s.handleSitemap)

	// Authoring.
	s.mux.HandleFunc("GET /mine", s.requireAuthor(s.handleMine))
	s.mux.HandleFunc("GET /new", s.requireAuthor(s.handleNewForm))
	s.mux.HandleFunc("POST /new", s.requireAuthor(s.handleCreate))
	s.mux.HandleFunc("GET /p/{id}/edit", s.requireAuthor(s.handleEditForm))
	s.mux.HandleFunc("POST /p/{id}/edit", s.requireAuthor(s.handleUpdate))
	s.mux.HandleFunc("POST /p/{id}/publish", s.requireAuthor(s.statusHandler(store.StatusPublished)))
	s.mux.HandleFunc("POST /p/{id}/unpublish", s.requireAuthor(s.statusHandler(store.StatusDraft)))
	s.mux.HandleFunc("POST /p/{id}/archive", s.requireAuthor(s.statusHandler(store.StatusArchived)))
	s.mux.HandleFunc("POST /p/{id}/delete", s.requireAuthor(s.handleDelete))
	s.mux.HandleFunc("POST /upload", s.requireAuthor(s.handleUpload))

	// Admin.
	s.mux.HandleFunc("GET /admin/blogs", s.requireAdmin(s.handleAdminBlogs))
	s.mux.HandleFunc("POST /admin/blogs", s.requireAdmin(s.handleAdminBlogCreate))
	s.mux.HandleFunc("POST /admin/blogs/{id}", s.requireAdmin(s.handleAdminBlogUpdate))
	s.mux.HandleFunc("POST /admin/blogs/{id}/delete", s.requireAdmin(s.handleAdminBlogDelete))
}

// renderPage renders a full page (templates/pages/<page>) wrapped in the
// base view, which supplies ui chrome.
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, page string, status int, view BaseView) {
	out, err := s.templates.Execute("pages/"+page, view)
	if err != nil {
		s.log.Error("render page", "page", page, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(out); err != nil { //nolint:gosec // G705: out is html/template output — contextually escaped
		s.log.Warn("render: write", "page", page, "err", err)
	}
}

// absURL builds an absolute URL for a module-relative path when BaseURL is
// configured, else a site-relative one. Feed documents and JSON-LD want
// absolute URLs; HTML href attributes work either way.
func (s *Server) absURL(rel string) string {
	return s.baseURL + s.basePath + rel
}

// cacheControl wraps an http.Handler with a Cache-Control header.
func cacheControl(value string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		h.ServeHTTP(w, r)
	})
}

// withSuiteAssets serves infodancer/ui's assets under "ui/" and mdedit's
// under "mdedit/" in front of blog's own embedded static files, so
// base.gohtml can link the shared CSS/JS at {BasePath}/static/... without
// the host mounting either module itself.
func withSuiteAssets(own fs.FS) fs.FS {
	return suiteRoutedFS{own: own, ui: uiassets.AssetsFS(), mdedit: mdedit.AssetsFS()}
}

type suiteRoutedFS struct {
	own, ui, mdedit fs.FS
}

func (u suiteRoutedFS) Open(name string) (fs.File, error) {
	switch {
	case name == "ui" || strings.HasPrefix(name, "ui/"):
		return u.ui.Open(strings.TrimPrefix(strings.TrimPrefix(name, "ui"), "/"))
	case name == "mdedit" || strings.HasPrefix(name, "mdedit/"):
		return u.mdedit.Open(strings.TrimPrefix(strings.TrimPrefix(name, "mdedit"), "/"))
	}
	return u.own.Open(name)
}
