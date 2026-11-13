package apitoken

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/infodancer/ui"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Scope is one permission a token can carry. The host defines its scopes; a
// scope narrows what a token may do, and the user's own permissions in the
// host still apply on top of it.
type Scope struct {
	// Name is the identifier checked by [RequireScope], e.g. "notes:write".
	Name string
	// Description is shown next to the scope on the account page.
	Description string
}

// Config is the host-supplied configuration for a [Module].
type Config struct {
	// DB is the host's pool. The module never opens its own connection.
	DB *pgxpool.Pool

	// AutoMigrate runs the embedded migrations during [New].
	AutoMigrate bool

	// Prefix starts every token the module mints, e.g. "abc_pat_". It lets
	// [Module.Dispatch] recognise the module's tokens without a database
	// round trip, and makes a leaked token recognisable to people and to
	// secret scanners. Required: 2-16 characters of [a-z0-9_], ending in "_".
	Prefix string

	// Scopes lists every scope a token may be minted with. Required.
	Scopes []Scope

	// Authenticated turns a verified token into the request context the
	// host's own session middleware would have built for that user, so every
	// authorization check behind it runs unchanged. Return an error wrapping
	// [ErrNoUser] when the token's user no longer exists (the request gets a
	// 401); any other error is treated as a server fault (503). Required by
	// [Module.Dispatch].
	Authenticated func(ctx context.Context, t *Token) (context.Context, error)

	// CurrentUser identifies the signed-in user for the account page. When
	// nobody is signed in it writes its own response (typically a redirect to
	// login) and returns ok=false. Required by [Module.Handler].
	CurrentUser func(w http.ResponseWriter, r *http.Request) (userKey string, ok bool)

	// BasePath is where the host mounts [Module.Handler], e.g.
	// "/account/tokens". Forms post back under it. Required by
	// [Module.Handler].
	BasePath string

	// Chrome supplies the host's page chrome per request. nil renders a bare
	// ui/document page.
	Chrome func(*http.Request) ui.DocumentData

	// Header, when set, returns host markup rendered at the top of the
	// account page's content, above the page's own title -- typically the
	// host's account heading and section tabs, so the page sits among the
	// host's other account pages. The page title then renders as an h2,
	// leaving the host's markup to supply the page's one h1. nil means no
	// header and an h1 title.
	Header func(*http.Request) template.HTML

	// CSRF tunes the account page's bundled same-origin check.
	CSRF CSRFConfig

	// Logger is optional; a nil logger discards.
	Logger *slog.Logger
}

// CSRFConfig controls the account page's bundled CSRF check.
type CSRFConfig struct {
	// Disable turns the check off, for a host that enforces CSRF on every
	// state-changing request itself.
	Disable bool
}

var prefixPattern = regexp.MustCompile(`^[a-z0-9_]{1,15}_$`)

func (c *Config) applyDefaults() {
	if c.Logger == nil {
		c.Logger = slog.New(slog.DiscardHandler)
	}
}

func (c *Config) validate() error {
	if c.DB == nil {
		return errors.New("apitoken: Config.DB is required")
	}
	if !prefixPattern.MatchString(c.Prefix) || len(c.Prefix) < 2 {
		return fmt.Errorf("apitoken: Config.Prefix %q must be 2-16 characters of [a-z0-9_] ending in \"_\"", c.Prefix)
	}
	if len(c.Scopes) == 0 {
		return errors.New("apitoken: Config.Scopes is required")
	}
	seen := map[string]bool{}
	for _, s := range c.Scopes {
		if s.Name == "" {
			return errors.New("apitoken: a Config.Scopes entry has no Name")
		}
		if seen[s.Name] {
			return fmt.Errorf("apitoken: scope %q listed twice", s.Name)
		}
		seen[s.Name] = true
	}
	return nil
}
