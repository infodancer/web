package faq

import (
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/ui"
)

// Config captures all wiring the host application supplies to [New]. Most
// fields have sensible defaults; only DB and Resolver are required.
type Config struct {
	// DB is the Postgres connection pool. Required.
	DB *pgxpool.Pool

	// Resolver translates an incoming request into a [*User]. Required.
	Resolver UserResolver

	// Logger is used for module-level structured logging. Defaults to
	// [slog.Default].
	Logger *slog.Logger

	// BasePath is the URL prefix at which the module is mounted (e.g. "/faq").
	// Used for link generation. Defaults to "/faq".
	BasePath string

	// SiteName is the host application's display name, surfaced in the
	// default nav as the "back to {SiteName}" brand link. Optional; empty
	// renders as "FAQ" so the chrome remains usable when the module is
	// run standalone.
	SiteName string

	// HomeURL is the URL the brand link points to. Defaults to "/" so
	// the user lands on the host's landing page; set explicitly when the
	// module is mounted under a subpath where "/" is not the host root.
	HomeURL string

	// LogoURL is the optional URL of a small brand mark rendered in the
	// nav alongside SiteName. Empty omits the image entirely.
	LogoURL string

	// SignInURL is the host's login URL. The module owns no login flow, so
	// when an anonymous visitor lands on an open question the detail page
	// shows a "sign in to post an answer" prompt that links here. Empty
	// renders the prompt as plain text (the host nav still carries its own
	// sign-in affordance), so a standalone faq stays usable.
	SignInURL string

	// Chrome, when set, supplies the full host page chrome (nav, footer, meta,
	// theme, analytics, asset base, head tags) per request as a ui.DocumentData
	// that pages render through ui/document. A host site sets this to inject its
	// own live, auth-aware navigation so the FAQ matches the rest of the site.
	// nil keeps the SiteName/HomeURL-derived default chrome, so a standalone
	// faq still renders a complete page. The module always adds its own section
	// nav (Tags / Search / Ask) as the left sidebar on top of whatever chrome
	// this returns.
	Chrome func(*http.Request) ui.DocumentData

	// Templates optionally overrides the embedded template filesystem. Layered:
	// names found here take precedence over the module's defaults.
	Templates fs.FS

	// Static optionally overrides the embedded static-asset filesystem.
	Static fs.FS

	// CSRF tunes (or disables) the bundled double-submit CSRF middleware.
	CSRF CSRFConfig

	// LinkRel is the rel= value applied to off-site links in rendered Markdown.
	// Defaults to "nofollow ugc".
	LinkRel string

	// PageSize is the default page size for list endpoints. Clamped to
	// [1, 100] at request time. Defaults to 25.
	PageSize int

	// EditGracePeriod is how long after creation the original author may
	// edit their own post. Admins are always allowed. Defaults to 10 minutes;
	// set to zero to disable non-admin edits entirely.
	EditGracePeriod time.Duration

	// AutoMigrate, if true, causes [New] to run [Module.Migrate]. Defaults to
	// false — explicit migration is preferred.
	AutoMigrate bool
}

// CSRFConfig controls the module's bundled CSRF middleware.
//
// The middleware is a same-origin guard: it compares the Origin (or
// Referer, as fallback) host on state-changing requests against
// r.Host and rejects mismatches with 403. It does not issue or validate
// tokens — that role is played by the host application's SameSite=Lax
// session cookie, which the middleware presupposes. See
// internal/handler/csrf.go for the rationale and behavior.
type CSRFConfig struct {
	// Disable skips the bundled middleware. Use when the host application
	// already runs an equivalent same-origin guard at a higher layer.
	Disable bool
}

func (c *Config) applyDefaults() {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.BasePath == "" {
		c.BasePath = "/faq"
	}
	if c.HomeURL == "" {
		c.HomeURL = "/"
	}
	if c.LinkRel == "" {
		c.LinkRel = "nofollow ugc"
	}
	if c.PageSize == 0 {
		c.PageSize = 25
	}
	if c.EditGracePeriod == 0 {
		c.EditGracePeriod = 10 * time.Minute
	}
}
