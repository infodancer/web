package claim

import (
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultTTL is how long an unconsumed invitation stays valid.
//
// Ninety days leaves room for outreach that gets answered weeks after it was
// sent, which is the normal case for cold or semi-warm contact. Past that
// horizon an unused link sitting in an old mailbox is a standing credential for
// taking over a listing, and re-issuing costs the host nothing.
const DefaultTTL = 90 * 24 * time.Hour

// Config is the host-supplied configuration for a claim [Module].
type Config struct {
	// DB is the host's pool. The module never opens its own connection.
	DB *pgxpool.Pool

	// TTL overrides [DefaultTTL] for newly issued invitations.
	TTL time.Duration

	// AutoMigrate runs the embedded migrations during [New].
	AutoMigrate bool

	// Logger is optional; a nil logger discards.
	Logger *slog.Logger
}

func (c *Config) applyDefaults() {
	if c.TTL <= 0 {
		c.TTL = DefaultTTL
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.DiscardHandler)
	}
}

func (c *Config) validate() error {
	if c.DB == nil {
		return errors.New("claim: Config.DB is required")
	}
	return nil
}
