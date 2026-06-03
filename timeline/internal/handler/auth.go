package handler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"

	"github.com/infodancer/web/timeline/db"
	"github.com/infodancer/web/timeline/timelineuser"
)

// resolveUserMiddleware resolves the request's user once and stashes it (or nil
// for anonymous) in the context for every downstream handler. A resolver error
// is treated as anonymous rather than failing the request: public timelines
// must stay readable even with a stale or malformed token. Admin-only routes
// enforce identity separately via [Server.requireAdmin].
func (s *Server) resolveUserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.resolver.Resolve(r.Context(), r)
		if err != nil {
			s.log.Warn("resolve user", "err", err)
			u = nil
		}
		ctx := timelineuser.ContextWithUser(r.Context(), u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdmin wraps an admin-only handler. Anonymous requests get a 401 with a
// WWW-Authenticate header so the host's login flow can intercept; authenticated
// non-admins get 403. On success it syncs the local timeline_users row and
// invokes h.
func (s *Server) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := timelineuser.UserFromContext(r.Context())
		if u == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="timeline", error="login_required"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !u.Admin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if _, err := s.resolveLocalUser(r.Context(), u); err != nil {
			s.log.Error("resolve local user", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h(w, r)
	}
}

// canViewTimeline reports whether the request's user may view tl. Public
// timelines are visible to everyone; non-public ones only to admins.
func (s *Server) canViewTimeline(ctx context.Context, public bool) bool {
	if public {
		return true
	}
	u := timelineuser.UserFromContext(ctx)
	return u != nil && u.Admin
}

// subHash returns the canonical sub_hash for an OIDC user (sha256 of "iss:sub").
func subHash(u *timelineuser.User) []byte {
	h := sha256.Sum256([]byte(u.Issuer + ":" + u.Subject))
	return h[:]
}

// resolveLocalUser ensures the resolved user has a matching timeline_users row
// (creating on first sight, syncing display_name and is_admin each call) and
// returns it.
func (s *Server) resolveLocalUser(ctx context.Context, u *timelineuser.User) (db.TimelineUser, error) {
	row, err := s.store.UpsertUser(ctx, db.UpsertUserParams{
		SubHash:     subHash(u),
		DisplayName: u.DisplayName,
	})
	if err != nil {
		return db.TimelineUser{}, fmt.Errorf("upsert user: %w", err)
	}
	if row.IsAdmin != u.Admin {
		if err := s.store.SetUserAdmin(ctx, db.SetUserAdminParams{ID: row.ID, IsAdmin: u.Admin}); err != nil {
			return db.TimelineUser{}, fmt.Errorf("set admin: %w", err)
		}
		row.IsAdmin = u.Admin
	}
	return row, nil
}
