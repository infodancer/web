package handler

import (
	"net/http"
	"net/url"
)

// csrfGuard rejects state-changing requests whose Origin (or Referer fallback)
// host doesn't match the request Host. Search is read-only today, so the guard
// is effectively a no-op on GET; it is present and configurable so a host that
// later mounts a state-changing admin endpoint inherits the protection. Modeled
// on the faq/osg same-origin guard.
//
// Safe methods (GET/HEAD/OPTIONS) bypass the check. State-changing requests
// that supply neither Origin nor Referer, or an Origin of literal "null", fail
// closed.
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

// sameOriginRequest reports whether r's Origin or Referer names a host equal to
// r.Host. Prefers Origin; falls back to Referer. Returns false when neither is
// present, when either parses unsafely, or when Origin is the literal "null".
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
