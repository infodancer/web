package timeline

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/media"
	"github.com/infodancer/web/timeline/timelineuser"
)

// Config captures everything the host application supplies to [New]. Only DB
// and Resolver are required; the rest have sensible defaults.
type Config struct {
	// DB is the Postgres connection pool, owned by the host. Required.
	DB *pgxpool.Pool

	// Resolver translates an incoming request into a [*timelineuser.User].
	// Required. Returning (nil, nil) is anonymous; the module treats anonymous
	// requests as read-only access to public timelines.
	Resolver timelineuser.UserResolver

	// Logger is used for module-level structured logging. Defaults to
	// [slog.Default].
	Logger *slog.Logger

	// BasePath is the URL prefix the module is mounted at (e.g. "/timeline").
	// Used for link generation. Defaults to "/timeline".
	BasePath string

	// BaseURL is the externally-visible origin used to build absolute URLs in
	// the sitemap and Atom feed (e.g. "https://example.org"). Empty falls back
	// to the request's Host header.
	BaseURL string

	// SignInURL is the host's login URL, surfaced to anonymous visitors. The
	// module owns no login flow.
	SignInURL string

	// Blobs is the media blob store portraits are read from and written to.
	// Optional: nil disables portrait storage and serving.
	Blobs media.BlobStore

	// Objects records portrait object metadata in media. Optional.
	Objects *media.Store

	// AutoMigrate, if true, runs [Module.Migrate] during [New]. Defaults to
	// false — explicit migration is preferred.
	AutoMigrate bool
}

func (c *Config) applyDefaults() {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.BasePath == "" {
		c.BasePath = "/timeline"
	}
}
