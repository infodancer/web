package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/infodancer/web/faq/faquser"
	"github.com/infodancer/web/faq/store"
)

// Test-only exports of package-private symbols. Naming convention is
// Export* so the go test runner doesn't interpret them as test
// functions (Test* would clash). Only consumed by handler_test files.

func ExportRequireUser(s *Server, w http.ResponseWriter, r *http.Request) (*faquser.User, bool) {
	return s.requireUser(w, r)
}

func ExportCanEditAs(s *Server, u *faquser.User, localUserID int64, authorID *int64, createdAt time.Time, parentLocked bool) bool {
	return s.canEditAs(u, localUserID, authorID, createdAt, parentLocked)
}

func ExportRequireAdmin(s *Server, w http.ResponseWriter, r *http.Request) (*faquser.User, bool) {
	return s.requireAdmin(w, r)
}

func ExportCanAccept(s *Server, u *faquser.User, localUserID int64, q store.FaqQuestions) bool {
	return s.canAccept(u, localUserID, q)
}

func ExportCanComment(s *Server, u *faquser.User, parentLocked bool) bool {
	return s.canComment(u, parentLocked)
}

func ExportResolveLocalUser(ctx context.Context, s *Server, u *faquser.User) (store.FaqUsers, error) {
	return s.resolveLocalUser(ctx, u)
}
