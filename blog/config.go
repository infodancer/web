package blog

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	ui "github.com/infodancer/ui"

	"github.com/infodancer/web/blog/bloguser"
	"github.com/infodancer/web/blog/internal/handler"
	"github.com/infodancer/web/media"
)

// AuthorInfo is the structured display identity the host's profile system
// knows about a user. Slug becomes the author's URL segment (a profile
// handle, typically); BioHTML must already be sanitized by the host's own
// pipeline (e.g. profile's bio renderer) — blog emits it verbatim.
type AuthorInfo = handler.AuthorInfo

// AuthorDirectory resolves a host user key to profile-backed display
// identity. (nil, nil) means the host knows nothing extra about the user;
// blog then falls back to the resolver's display name. Hosts running the
// infodancer/web/profile component back this with its store.
type AuthorDirectory = handler.AuthorDirectory

// SearchDocument is one published post as the host's search index sees it.
// URLs are absolute when Config.BaseURL is set.
type SearchDocument = handler.SearchDocument

// Indexer is the host-wired search hook, called on every post lifecycle
// transition: published posts are (re)indexed, everything else removed.
// Hosts running infodancer/web/search adapt its Module in a few lines:
//
//	type searchIndexer struct{ m *search.Module }
//
//	func (a searchIndexer) IndexPost(ctx context.Context, d blog.SearchDocument) error {
//		_, err := a.m.Index(ctx, search.IngestDocument{
//			Source: "blog", SourceName: "posts", ExternalID: d.ExternalID,
//			URL: d.URL, Title: d.Title, Summary: d.Summary, Tags: d.Tags,
//			Body: d.BodyMD, BodyKind: search.BodyMarkdown, PublishedAt: d.PublishedAt,
//		})
//		return err
//	}
//
//	func (a searchIndexer) DeletePost(ctx context.Context, id string) error {
//		_, err := a.m.Delete(ctx, "blog", "posts", id)
//		return err
//	}
//
// Indexing failures are logged, never user-facing — search lags rather
// than blocking a save.
type Indexer = handler.Indexer

// Config captures everything the host application supplies to [New]. Only
// DB and Resolver are required; the rest have sensible defaults.
type Config struct {
	// DB is the Postgres connection pool, owned by the host. Required.
	DB *pgxpool.Pool

	// Resolver translates an incoming request into a [*bloguser.User].
	// Required. Returning (nil, nil) is anonymous read-only access. The
	// blogauthz package adapts the infodancer/authz role store into this
	// shape, which is how authorship stays invitation-only.
	Resolver bloguser.Resolver

	// Logger is used for module-level structured logging. Defaults to
	// [slog.Default].
	Logger *slog.Logger

	// BasePath is the URL prefix the module is mounted at. Used for link
	// generation. Defaults to "/blog".
	BasePath string

	// BaseURL is the site's absolute origin (e.g. "https://example.org",
	// no trailing slash). Feeds, the sitemap, and JSON-LD need it to emit
	// absolute URLs; when empty those documents degrade to site-relative
	// URLs, which most consumers reject. Set it in production.
	BaseURL string

	// SiteName titles the blog chrome, feeds, and JSON-LD. Defaults to
	// "Blog". SiteDescription feeds the meta description, combined feeds,
	// and the Blog JSON-LD node.
	SiteName        string
	SiteDescription string

	// HomeURL is the back-to-site link in the default chrome. SignInURL is
	// the host's login URL, surfaced to anonymous visitors.
	HomeURL   string
	SignInURL string

	// Chrome, when set, supplies the full host page chrome per request as
	// a ui.DocumentData that base.gohtml renders through ui/document. nil
	// uses the SiteName/HomeURL-derived default, so a standalone mount
	// still renders a complete page.
	Chrome func(*http.Request) ui.DocumentData

	// Authors resolves author display identity (handle, display name,
	// avatar, bio) from the host's profile system. Optional; without it
	// the author registry is seeded from resolver display names.
	Authors AuthorDirectory

	// AuthorCard, when set, renders the byline card on post pages (e.g.
	// profile.Module.Card). nil falls back to a plain text byline.
	AuthorCard func(ctx context.Context, userKey string) (template.HTML, error)

	// Indexer is the search hook; see [Indexer]. nil disables indexing.
	Indexer Indexer

	// Blobs and Objects are the media stores inline image uploads are
	// written to. Both optional, but upload is enabled only when both are
	// set; without them the editor's image button stays plain
	// link-insertion.
	Blobs   media.BlobStore
	Objects *media.Store

	// Media is the upload pipeline configuration: Category (defaults to
	// "blog"), MaxUploadBytes, MaxPixels, Thumbnails, and PublicBaseURL.
	// PublicBaseURL must point at where the host serves the category from
	// its media mount (e.g. "/media/blog") — the canonical URL media
	// returns is what lands in the markdown.
	Media media.Config

	// PageSize is the listing page length (default 20). FeedSize is the
	// entry count in every feed document (default 20).
	PageSize int
	FeedSize int

	// CSRF tunes (or disables) the bundled same-origin CSRF middleware.
	CSRF CSRFConfig

	// AutoMigrate, if true, runs [Module.Migrate] during [New]. Defaults
	// to false — explicit migration is preferred.
	AutoMigrate bool
}

// CSRFConfig controls the module's bundled CSRF middleware.
type CSRFConfig struct {
	// Disable skips the bundled middleware. Use only when the host
	// application already runs an equivalent same-origin guard at a
	// higher layer.
	Disable bool
}

func (c *Config) applyDefaults() {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.BasePath == "" {
		c.BasePath = "/blog"
	}
	if c.SiteName == "" {
		c.SiteName = "Blog"
	}
	if c.Media.Category == "" {
		c.Media.Category = "blog"
	}
}
