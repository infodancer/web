package httpsec

import (
	"net/http"
	"net/url"
	"strings"
)

// CSRFConfig tunes [CSRF].
type CSRFConfig struct {
	// Disable turns the guard off, for a host that enforces CSRF itself.
	Disable bool
	// TrustedOrigins are extra hosts (with port, if any) whose requests are
	// accepted as same-site, for example an admin hostname that posts to the
	// public one. Compared case-insensitively.
	TrustedOrigins []string
}

// CSRF rejects state-changing requests that do not originate from the site
// itself. For any method other than GET, HEAD and OPTIONS it requires the
// Origin header (or, when Origin is absent, the Referer) to name the request's
// own host. A request with neither header, an Origin of "null", an unparseable
// value, or one carrying userinfo is refused with 403. Browsers send Origin or
// Referer on every cross-site form post, so failing closed costs nothing real.
//
// This backs up, and does not replace, a SameSite=Lax session cookie.
func CSRF(cfg CSRFConfig) func(http.Handler) http.Handler {
	trusted := make(map[string]bool, len(cfg.TrustedOrigins))
	for _, o := range cfg.TrustedOrigins {
		trusted[strings.ToLower(o)] = true
	}
	return func(next http.Handler) http.Handler {
		if cfg.Disable {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if !sameSite(r, trusted) {
				http.Error(w, "forbidden: cross-origin request rejected", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// sameSite prefers Origin, the explicit signal, and falls back to Referer.
func sameSite(r *http.Request, trusted map[string]bool) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		return hostAllowed(origin, r.Host, trusted)
	}
	if referer := r.Header.Get("Referer"); referer != "" {
		return hostAllowed(referer, r.Host, trusted)
	}
	return false
}

func hostAllowed(raw, requestHost string, trusted map[string]bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	return host == strings.ToLower(requestHost) || trusted[host]
}
