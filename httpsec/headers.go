package httpsec

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Policy is a Content-Security-Policy under construction. Directives keep
// their insertion order so the rendered header is stable. Methods mutate and
// return the receiver, and panic on a malformed directive or source: a policy
// is built at startup from literals, and a value containing ";" or whitespace
// would otherwise inject a directive.
type Policy struct {
	order []string
	srcs  map[string][]string
}

// DefaultPolicy returns a new strict policy: everything from 'self' only, no
// inline script or style, no plugins, no framing, no base or form retargeting.
// Each call returns an independent copy.
func DefaultPolicy() *Policy {
	p := &Policy{srcs: make(map[string][]string)}
	for _, d := range []struct {
		name string
		src  []string
	}{
		{"default-src", []string{"'self'"}},
		{"script-src", []string{"'self'"}},
		{"style-src", []string{"'self'"}},
		{"img-src", []string{"'self'", "data:"}},
		{"font-src", []string{"'self'"}},
		{"connect-src", []string{"'self'"}},
		{"form-action", []string{"'self'"}},
		{"base-uri", []string{"'self'"}},
		{"object-src", []string{"'none'"}},
		{"frame-ancestors", []string{"'none'"}},
	} {
		p.order = append(p.order, d.name)
		p.srcs[d.name] = slices.Clone(d.src)
	}
	return p
}

// Allow appends sources to a directive, creating it if needed and skipping
// duplicates. Adding a real source to a directive that is 'none' drops the
// 'none', since the two cannot coexist.
func (p *Policy) Allow(directive string, sources ...string) *Policy {
	checkDirective(directive)
	for _, s := range sources {
		checkSource(s)
	}
	if _, ok := p.srcs[directive]; !ok {
		p.order = append(p.order, directive)
	}
	cur := p.srcs[directive]
	if len(sources) > 0 && len(cur) == 1 && cur[0] == "'none'" {
		cur = nil
	}
	for _, s := range sources {
		if !slices.Contains(cur, s) {
			cur = append(cur, s)
		}
	}
	p.srcs[directive] = cur
	return p
}

// Set replaces a directive's sources outright, creating it if needed.
func (p *Policy) Set(directive string, sources ...string) *Policy {
	checkDirective(directive)
	for _, s := range sources {
		checkSource(s)
	}
	if _, ok := p.srcs[directive]; !ok {
		p.order = append(p.order, directive)
	}
	p.srcs[directive] = slices.Clone(sources)
	return p
}

// String renders the header value.
func (p *Policy) String() string {
	parts := make([]string, 0, len(p.order))
	for _, name := range p.order {
		parts = append(parts, strings.TrimSpace(name+" "+strings.Join(p.srcs[name], " ")))
	}
	return strings.Join(parts, "; ")
}

func checkDirective(d string) {
	if d == "" {
		panic("httpsec: empty CSP directive")
	}
	for _, r := range d {
		lower, digit := r >= 'a' && r <= 'z', r >= '0' && r <= '9'
		if !lower && !digit && r != '-' {
			panic(fmt.Sprintf("httpsec: invalid CSP directive %q", d))
		}
	}
}

func checkSource(s string) {
	if s == "" {
		panic("httpsec: empty CSP source")
	}
	for _, r := range s {
		if r == ';' || r == ',' || unicode.IsSpace(r) || unicode.IsControl(r) {
			panic(fmt.Sprintf("httpsec: invalid CSP source %q", s))
		}
	}
}

// HeaderConfig tunes [Headers]. The zero value is the strict default set.
type HeaderConfig struct {
	// Policy is the Content-Security-Policy. Nil means [DefaultPolicy]. The
	// policy is rendered once when Headers is called; later changes to it
	// have no effect.
	Policy *Policy

	// DisableHSTS omits Strict-Transport-Security, for plain-HTTP development.
	DisableHSTS bool
	// HSTSMaxAge is the HSTS lifetime. Zero means two years.
	HSTSMaxAge time.Duration
	// HSTSPreload adds the preload token. Only set it for a domain that has
	// been submitted to the preload list.
	HSTSPreload bool

	// FrameOptions is X-Frame-Options. Empty means DENY.
	FrameOptions string
	// ReferrerPolicy is Referrer-Policy. Empty means strict-origin-when-cross-origin.
	ReferrerPolicy string
	// PermissionsPolicy is Permissions-Policy. Empty denies the powerful
	// browser features a content site does not use.
	PermissionsPolicy string
}

const (
	defaultHSTSMaxAge        = 2 * 365 * 24 * time.Hour
	defaultFrameOptions      = "DENY"
	defaultReferrerPolicy    = "strict-origin-when-cross-origin"
	defaultPermissionsPolicy = "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
)

// Headers sets the security response headers before calling the next handler,
// so they ride on every response including errors. A handler may overwrite one
// for its own response, for example to tighten the CSP on user-supplied files.
func Headers(cfg HeaderConfig) func(http.Handler) http.Handler {
	policy := cfg.Policy
	if policy == nil {
		policy = DefaultPolicy()
	}
	csp := policy.String()

	hsts := ""
	if !cfg.DisableHSTS {
		age := cfg.HSTSMaxAge
		if age == 0 {
			age = defaultHSTSMaxAge
		}
		hsts = "max-age=" + strconv.FormatInt(int64(age/time.Second), 10) + "; includeSubDomains"
		if cfg.HSTSPreload {
			hsts += "; preload"
		}
	}
	frame := orDefault(cfg.FrameOptions, defaultFrameOptions)
	referrer := orDefault(cfg.ReferrerPolicy, defaultReferrerPolicy)
	perms := orDefault(cfg.PermissionsPolicy, defaultPermissionsPolicy)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			if hsts != "" {
				h.Set("Strict-Transport-Security", hsts)
			}
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", frame)
			h.Set("Referrer-Policy", referrer)
			h.Set("Permissions-Policy", perms)
			next.ServeHTTP(w, r)
		})
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// Chain wraps h in the given middleware. The first middleware is outermost, so
// it sees the request first.
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
