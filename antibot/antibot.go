package antibot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/infodancer/logging/httplog"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/antibot/db"
)

const (
	defaultHoneypotField = "website"
	defaultTokenField    = "antibot_token" //nolint:gosec // G101 false positive: the name of a form field, not a credential
	defaultMinFill       = 3 * time.Second
	defaultMaxAge        = 2 * time.Hour

	// minSecretLen is the shortest secret New accepts: 32 bytes, the size of
	// the HMAC-SHA256 output it keys.
	minSecretLen = 32
)

var (
	defaultIPLimit    = Limit{Max: 5, Window: time.Hour}
	defaultEmailLimit = Limit{Max: 3, Window: 24 * time.Hour}
)

// ErrWeakSecret is returned by New when Config.Secret is shorter than 32 bytes.
var ErrWeakSecret = fmt.Errorf("antibot: secret must be at least %d bytes", minSecretLen)

// ErrFormNotParsed is returned by Check when the request's form has not been
// parsed. Check reads the values the handler already parsed and never touches
// the body, so this is a wiring mistake rather than a bot.
var ErrFormNotParsed = errors.New("antibot: Check needs a parsed form; call ParseForm or ParseMultipartForm first")

// A Reason says why a request was rejected. The strings are stable: they are
// logged, and a caller may branch on them.
type Reason string

// The reasons Check reports.
const (
	ReasonHoneypot     Reason = "honeypot"
	ReasonTokenMissing Reason = "token_missing"
	ReasonTokenInvalid Reason = "token_invalid"
	ReasonTooFast      Reason = "too_fast"
	ReasonTokenExpired Reason = "token_expired"
	ReasonRateLimited  Reason = "rate_limited"
	ReasonInvalidEmail Reason = "invalid_email"
	ReasonNoMX         Reason = "no_mx"
	ReasonUnavailable  Reason = "unavailable"
)

// A Verdict is Check's answer. When Allowed is false, Reason says why, and
// Silent says the caller should pretend the submission succeeded.
type Verdict struct {
	Allowed bool
	Silent  bool
	Reason  Reason
}

// Config configures a Guard. DB and Secret are required; everything else has a
// default.
type Config struct {
	// DB is the host's pool. The limiter's counters live there, in tables
	// prefixed antibot_; run Migrate against it first.
	DB *pgxpool.Pool

	// Secret signs timing tokens and keys the limiter's hashes. At least 32
	// random bytes, kept out of the repository. Rotating it invalidates tokens
	// on forms already rendered and resets every rate-limit budget.
	Secret []byte

	// Proxies decides which peers' X-Forwarded-For is believed. The zero value
	// trusts nothing, so the peer address is used.
	Proxies httplog.Proxies

	// Logger receives rejections at info. Nil uses slog.Default().
	Logger *slog.Logger

	// RequestID reads the request id from the context, so a rejection shares
	// the id of the access line for that request. Nil logs no request id.
	RequestID func(context.Context) string

	// HoneypotField and TokenField name the form fields. Defaults "website" and
	// "antibot_token".
	HoneypotField string
	TokenField    string

	// MinFill is the fastest a person can fill the form in; MaxAge is how long
	// a rendered form stays valid. Defaults 3 seconds and 2 hours.
	MinFill time.Duration
	MaxAge  time.Duration

	// IPLimit and EmailLimit bound submissions per form. The zero value takes
	// the default (5 an hour per IP, 3 a day per email); NoLimit disables one.
	IPLimit    Limit
	EmailLimit Limit

	// LookupMX resolves mail servers. Nil uses net.DefaultResolver.
	LookupMX func(ctx context.Context, domain string) ([]*net.MX, error)

	// Now is the clock. Nil uses time.Now.
	Now func() time.Time
}

// Guard checks submissions. It is safe for concurrent use.
type Guard struct {
	store         db.Querier
	secret        []byte
	proxies       httplog.Proxies
	log           *slog.Logger
	requestID     func(context.Context) string
	honeypotField string
	tokenField    string
	minFill       time.Duration
	maxAge        time.Duration
	ipLimit       Limit
	emailLimit    Limit
	lookupMX      func(ctx context.Context, domain string) ([]*net.MX, error)
	now           func() time.Time
}

// New builds a Guard over the host's pool.
func New(cfg Config) (*Guard, error) {
	if cfg.DB == nil {
		return nil, errors.New("antibot: Config.DB is required")
	}
	return newGuard(cfg, db.New(cfg.DB))
}

// newGuard builds a Guard over any store, so the checks can be tested without
// a database.
func newGuard(cfg Config, store db.Querier) (*Guard, error) {
	if len(cfg.Secret) < minSecretLen {
		return nil, ErrWeakSecret
	}
	g := &Guard{
		store:         store,
		secret:        append([]byte(nil), cfg.Secret...),
		proxies:       cfg.Proxies,
		log:           cfg.Logger,
		requestID:     cfg.RequestID,
		honeypotField: orString(cfg.HoneypotField, defaultHoneypotField),
		tokenField:    orString(cfg.TokenField, defaultTokenField),
		minFill:       orDuration(cfg.MinFill, defaultMinFill),
		maxAge:        orDuration(cfg.MaxAge, defaultMaxAge),
		ipLimit:       orLimit(cfg.IPLimit, defaultIPLimit),
		emailLimit:    orLimit(cfg.EmailLimit, defaultEmailLimit),
		lookupMX:      cfg.LookupMX,
		now:           cfg.Now,
	}
	if g.log == nil {
		g.log = slog.Default()
	}
	if g.requestID == nil {
		g.requestID = func(context.Context) string { return "" }
	}
	if g.lookupMX == nil {
		g.lookupMX = net.DefaultResolver.LookupMX
	}
	if g.now == nil {
		g.now = time.Now
	}
	for name, l := range map[string]Limit{"IPLimit": g.ipLimit, "EmailLimit": g.emailLimit} {
		if l.Max > 0 && l.Window <= 0 {
			return nil, fmt.Errorf("antibot: %s has a limit but no window", name)
		}
	}
	return g, nil
}

// A CheckOption supplies what Check needs beyond the request.
type CheckOption func(*checkOptions)

type checkOptions struct {
	email    string
	hasEmail bool
}

// Email adds the submitted address to the check: its syntax, its per-address
// limit, and whether its domain accepts mail.
func Email(addr string) CheckOption {
	return func(o *checkOptions) {
		o.email = addr
		o.hasEmail = true
	}
}

// Check decides whether a submission to form may proceed. The request's form
// must already be parsed.
//
// A non-nil error means the limiter's store failed; the verdict is then a
// rejection with ReasonUnavailable, and the caller should treat the submission
// as failed rather than let it through.
func (g *Guard) Check(ctx context.Context, r *http.Request, form string, opts ...CheckOption) (Verdict, error) {
	if r.PostForm == nil {
		return Verdict{}, ErrFormNotParsed
	}
	var co checkOptions
	for _, o := range opts {
		o(&co)
	}
	ip := g.proxies.ClientIP(r)

	if r.PostForm.Get(g.honeypotField) != "" {
		return g.reject(ctx, form, ip, ReasonHoneypot, true), nil
	}
	if reason := g.verifyToken(form, r.PostForm.Get(g.tokenField)); reason != "" {
		return g.reject(ctx, form, ip, reason, false), nil
	}
	if v, done, err := g.count(ctx, form, ip, kindIP, ipKey(ip), g.ipLimit); done {
		return v, err
	}
	if !co.hasEmail {
		return Verdict{Allowed: true}, nil
	}

	addr, domain, ok := normalizeEmail(co.email)
	if !ok {
		return g.reject(ctx, form, ip, ReasonInvalidEmail, false), nil
	}
	if v, done, err := g.count(ctx, form, ip, kindEmail, addr, g.emailLimit); done {
		return v, err
	}
	if reason := g.mailDomain(ctx, form, domain); reason != "" {
		return g.reject(ctx, form, ip, reason, false), nil
	}
	return Verdict{Allowed: true}, nil
}

// Sweep deletes rate-limit windows that have expired and reports how many. The
// host schedules it; an unswept table only grows, it never miscounts.
func (g *Guard) Sweep(ctx context.Context) (int64, error) {
	n, err := g.store.Sweep(ctx, g.now())
	if err != nil {
		return 0, fmt.Errorf("antibot: sweep: %w", err)
	}
	return n, nil
}

// reject logs a rejection and returns its verdict. A rejection is the check
// working, not failing, so it is info: an operator reads it to follow up abuse,
// not to be paged. The token is never logged -- anyone reading the log could
// replay it.
func (g *Guard) reject(ctx context.Context, form, ip string, reason Reason, silent bool) Verdict {
	attrs := []any{"form", form, "reason", string(reason), "ip", ip}
	if silent {
		attrs = append(attrs, "silent", true)
	}
	if id := g.requestID(ctx); id != "" {
		attrs = append(attrs, "request_id", id)
	}
	g.log.InfoContext(ctx, "antibot_rejected", attrs...)
	return Verdict{Silent: silent, Reason: reason}
}

func orString(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func orDuration(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}
