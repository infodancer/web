package handler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"time"

	"github.com/infodancer/web/faq/faquser"
	"github.com/infodancer/web/faq/store"
)

// requireUser is the gate for every write-path handler. It resolves the
// current user, attaches them to r.Context, and returns (*User, true)
// on success. On failure (anonymous user, resolver error, etc.) it
// writes a 401 with WWW-Authenticate: Bearer and returns (nil, false).
//
// The module never owns a login flow. The header tells the host (and
// any layered htmx client) that the user must be authenticated; the
// host is expected to intercept and redirect to its own login page.
func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) (*faquser.User, bool) {
	u, ok := s.resolveUser(w, r)
	if !ok {
		// resolveUser already wrote a 401 for an invalid token.
		return nil, false
	}
	if u == nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="faq", error="login_required"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	return u, true
}

// requireAdmin extends requireUser with an additional admin check. Used
// by every M5 moderation endpoint. Non-admin authenticated users get
// 403 (the resource exists, they just can't act on it); anon users
// still get the 401 + WWW-Authenticate from requireUser so the host
// app's login flow kicks in.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (*faquser.User, bool) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return nil, false
	}
	if !u.Admin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, false
	}
	return u, true
}

// canEditAs reports whether u is allowed to edit a post (question,
// answer, or comment) authored by authorID at createdAt. Rules per
// DESIGN.md §9.1, with the M5 tightening that locked questions freeze
// their entire subtree against non-admin edits:
//
//   - Admins always edit (any post, any time, regardless of parentLocked).
//   - The original author edits within EditGracePeriod after createdAt,
//     but only when the parent question is not locked.
//   - Anyone else is rejected.
//
// Callers pass parentLocked = (q.Status == FaqQuestionStatusLocked). For
// the question itself, q IS the parent — the same flag covers it.
//
// localUserID is u's faq_users.id (looked up by the caller via
// resolveLocalUser); the gate compares it to authorID directly.
func (s *Server) canEditAs(u *faquser.User, localUserID int64, authorID *int64, createdAt time.Time, parentLocked bool) bool {
	if u == nil {
		return false
	}
	if u.Admin {
		return true
	}
	if parentLocked {
		return false
	}
	if authorID == nil || *authorID != localUserID {
		return false
	}
	if s.deps.EditGracePeriod <= 0 {
		return false
	}
	return time.Since(createdAt) <= s.deps.EditGracePeriod
}

// canAccept reports whether u is allowed to set/clear the accepted
// answer on q. Rules per the M4 decision (see the M4 prep thread):
//
//   - Admins always accept.
//   - On native or unlocked questions, the question author accepts.
//   - On imported+locked questions the original author has no local
//     account, so only admins can accept.
//
// localUserID is u's faq_users.id (looked up by the caller).
func (s *Server) canAccept(u *faquser.User, localUserID int64, q store.FaqQuestions) bool {
	if u == nil {
		return false
	}
	if u.Admin {
		return true
	}
	if q.Status != store.FaqQuestionStatusOpen {
		return false
	}
	if q.AuthorUserID == nil || *q.AuthorUserID != localUserID {
		return false
	}
	return true
}

// canComment reports whether u may add a new comment under post.
// Rules:
//
//   - Anon rejected.
//   - Admin always permitted.
//   - On open questions/answers anyone authenticated may comment.
//   - On locked posts only admins may comment (the imported corpus is
//     an admin-only annotation surface).
func (s *Server) canComment(u *faquser.User, parentLocked bool) bool {
	if u == nil {
		return false
	}
	if u.Admin {
		return true
	}
	return !parentLocked
}

// subHash returns the canonical sub_hash for an OIDC user (sha256 of
// "iss:sub"). Used for faq_users.sub_hash lookups and inserts.
func subHash(u *faquser.User) []byte {
	h := sha256.Sum256([]byte(u.Issuer + ":" + u.Subject))
	return h[:]
}

// resolveLocalUser ensures the resolved [*faquser.User] has a matching
// faq_users row and returns it (creating on first sight, updating
// display_name + is_admin on each invocation). Every write-path handler
// runs this once after [Server.requireUser] so the rest of the handler
// can operate against a stable local user id.
func (s *Server) resolveLocalUser(ctx context.Context, u *faquser.User) (store.FaqUsers, error) {
	row, err := s.deps.Store.UpsertUser(ctx, store.UpsertUserParams{
		SubHash:     subHash(u),
		DisplayName: u.DisplayName,
	})
	if err != nil {
		return store.FaqUsers{}, fmt.Errorf("upsert user: %w", err)
	}
	// Sync admin bit on each authed call so a flag flip in the host
	// app's directory propagates without the user having to wait for
	// their next display_name change.
	if row.IsAdmin != u.Admin {
		if err := s.deps.Store.SetUserAdmin(ctx, store.SetUserAdminParams{
			ID: row.ID, IsAdmin: u.Admin,
		}); err != nil {
			return store.FaqUsers{}, fmt.Errorf("set admin: %w", err)
		}
		row.IsAdmin = u.Admin
	}
	return row, nil
}
