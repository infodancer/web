package handler

import (
	"net/http"

	"github.com/infodancer/web/profile/profileuser"
)

// resolveUserMiddleware resolves the request's user once and stashes it (or
// nil for anonymous) in the context for every downstream handler. A resolver
// error is treated as anonymous rather than failing the request: public
// profiles must stay readable even with a stale or malformed token. Write
// routes enforce identity separately via [Server.requireUser].
func (s *Server) resolveUserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.resolver.Resolve(r.Context(), r)
		if err != nil {
			s.log.Warn("resolve user", "err", err)
			u = nil
		}
		ctx := profileuser.ContextWithUser(r.Context(), u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireUser wraps a handler that needs an authenticated subject. Anonymous
// requests get a 401 with a WWW-Authenticate header so the host's login flow
// can intercept.
func (s *Server) requireUser(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := profileuser.UserFromContext(r.Context())
		if u == nil || u.Key == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="profile", error="login_required"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}
