package profile

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	ui "github.com/infodancer/ui"

	"github.com/infodancer/web/media"
	"github.com/infodancer/web/profile/internal/handler"
	"github.com/infodancer/web/profile/profileuser"
)

// Section is one host-owned block profile places on its pages: a title plus
// view and/or edit markup the host rendered itself. Per DESIGN.md §3 each
// section is a self-contained island — its forms post to host endpoints, its
// data lives in host tables, and profile never parses, validates, or stores
// its contents. An empty ViewHTML omits the section from the public page; an
// empty EditHTML omits it from the edit page.
type Section = handler.Section

// Config captures everything the host application supplies to [New]. Only DB
// and Resolver are required; the rest have sensible defaults.
type Config struct {
	// DB is the Postgres connection pool, owned by the host. Required.
	DB *pgxpool.Pool

	// Resolver translates an incoming request into a [*profileuser.User].
	// Required. Returning (nil, nil) is anonymous; the module treats
	// anonymous requests as read-only access to public profiles.
	Resolver profileuser.Resolver

	// Logger is used for module-level structured logging. Defaults to
	// [slog.Default].
	Logger *slog.Logger

	// BasePath is the URL prefix the module is mounted at (e.g. "/profile").
	// Used for link generation. Defaults to "/profile".
	BasePath string

	// SignInURL is the host's login URL, surfaced to anonymous visitors. The
	// module owns no login flow.
	SignInURL string

	// SiteName, HomeURL, LogoURL drive the default nav/footer brand chrome
	// used when Chrome is nil. All optional; SiteName defaults to "Profiles".
	SiteName string
	HomeURL  string
	LogoURL  string

	// Chrome, when set, supplies the full host page chrome per request as a
	// ui.DocumentData that base.gohtml renders through ui/document. nil uses
	// the SiteName/HomeURL-derived default, so a standalone mount still
	// renders a complete page.
	Chrome func(*http.Request) ui.DocumentData

	// Sections supplies host-owned profile sections, rendered after
	// profile's own basics on the public page (ViewHTML) and the edit page
	// (EditHTML). Called per request with the profile owner's user key; the
	// host gates per-viewer visibility itself. nil means no host sections.
	Sections func(ctx context.Context, r *http.Request, userKey string) []Section

	// Blobs and Objects are the media stores avatar uploads are written to.
	// Both optional, but avatar upload is enabled only when both are set;
	// without them profiles fall back to the generated identicon.
	Blobs   media.BlobStore
	Objects *media.Store

	// Avatar is the media pipeline configuration for uploads: Category
	// (defaults to "avatar"), MaxUploadBytes, MaxPixels, Thumbnails, and
	// PublicBaseURL. PublicBaseURL must point at where the host serves the
	// avatar category from its media mount (e.g. "/media/avatar") -- the
	// canonical URL media returns is what gets stored on the profile.
	Avatar media.Config

	// ReservedHandles extends the built-in reserved-handle set (route words,
	// authority names) with host-specific entries.
	ReservedHandles []string

	// CSRF tunes (or disables) the bundled same-origin CSRF middleware.
	CSRF CSRFConfig

	// AutoMigrate, if true, runs [Module.Migrate] during [New]. Defaults to
	// false -- explicit migration is preferred.
	AutoMigrate bool
}

// CSRFConfig controls the module's bundled CSRF middleware.
type CSRFConfig struct {
	// Disable skips the bundled middleware. Use only when the host
	// application already runs an equivalent same-origin guard at a higher
	// layer.
	Disable bool
}

func (c *Config) applyDefaults() {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.BasePath == "" {
		c.BasePath = "/profile"
	}
	if c.Avatar.Category == "" {
		c.Avatar.Category = "avatar"
	}
}
