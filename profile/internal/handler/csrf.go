package handler

import (
	"net/http"
	"net/url"
)

// csrfGuard rejects state-changing requests whose Origin (or Referer as
// fallback) host doesn't match the request's Host header.
//
// Modeled on the faq module's guard (itself modeled on osg's). The host
// application's session cookie already carries SameSite=Lax, which blocks
// the overwhelming majority of cross-site POSTs in modern browsers; this
// middleware covers the remaining cases (older browsers, sibling-origin
// scenarios, opaque-origin sandboxes) without requiring a token-bearing
// template field or a separate cookie the module would have to manage.
//
// Safe methods (GET/HEAD/OPTIONS) bypass the check. State-changing requests
// that supply neither Origin nor Referer fail closed -- every modern browser
// sends one or the other on a POST. Origin="null" (opaque-origin contexts
// like sandboxed iframes) also fails closed.
//
// The guard runs unconditionally unless Config.CSRF.Disable is set, in which
// case the host application is presumed to enforce CSRF itself.
func (s *Server) csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.csrfDisabled {
			next.ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if !sameOriginRequest(r) {
			http.Error(w, "forbidden: cross-origin request rejected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOriginRequest reports whether r's Origin or Referer header names a host
// equal to r.Host. Prefers Origin (the explicit signal browsers send for
// CORS-relevant requests); falls back to Referer when Origin is absent.
// Returns false when neither header is present, when either parses unsafely,
// or when Origin is the literal "null".
func sameOriginRequest(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		if origin == "null" {
			return false
		}
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" {
			return false
		}
		return u.Host == r.Host
	}
	if referer := r.Header.Get("Referer"); referer != "" {
		u, err := url.Parse(referer)
		if err != nil || u.Host == "" {
			return false
		}
		return u.Host == r.Host
	}
	return false
}
