package handler

import (
	"net/http"

	"github.com/infodancer/web/blog/bloguser"
)

// resolveUserMiddleware resolves the request's user once and stashes it (or
// nil for anonymous) in the context for every downstream handler. A
// resolver error is treated as anonymous on safe methods so the public read
// surface stays available with a stale or malformed token; write routes
// enforce identity separately via the require* wrappers.
func (s *Server) resolveUserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.resolver.Resolve(r.Context(), r)
		if err != nil {
			s.log.Warn("resolve user", "err", err)
			u = nil
		}
		ctx := bloguser.ContextWithUser(r.Context(), u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAuthor wraps handlers that need authoring rights: a signed-in user
// holding the author (or admin) role. Anonymous or unauthorized requests
// get 401/403 so the host's login flow can intercept.
func (s *Server) requireAuthor(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := bloguser.UserFromContext(r.Context())
		if u == nil || u.Key == "" {
			unauthorized(w)
			return
		}
		if !u.Author && !u.Admin {
			http.Error(w, "forbidden: authoring requires the blog author role", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

// requireAdmin wraps handlers that need the admin role.
func (s *Server) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := bloguser.UserFromContext(r.Context())
		if u == nil || u.Key == "" {
			unauthorized(w)
			return
		}
		if !u.Admin {
			http.Error(w, "forbidden: admin role required", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="blog", error="login_required"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// mayEditPost reports whether u may modify a post owned by authorKey.
func mayEditPost(u *bloguser.User, authorKey string) bool {
	if u == nil {
		return false
	}
	return u.Admin || (u.Author && u.Key == authorKey)
}
