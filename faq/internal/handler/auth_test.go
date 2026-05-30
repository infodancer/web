//go:build integration

package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/faquser"
	"github.com/infodancer/web/faq/internal/handler"
	"github.com/infodancer/web/faq/store"
)

// authedResolver returns a fixed user for every request. Tests use it
// to switch identities without faking JWT plumbing.
type authedResolver struct{ user *faquser.User }

func (a authedResolver) Resolve(_ context.Context, _ *http.Request) (*faquser.User, error) {
	return a.user, nil
}

// adminUser / regularUser build a [*faquser.User] whose sub_hash matches
// the one [seedQuestion] computes for the same name. The issuer is
// "https://test" (no .example) to match list_test.go's seed helpers so a
// seeded question's author_user_id resolves to the same local row that
// resolveLocalUser produces for the User.
func adminUser(name string) *faquser.User {
	return &faquser.User{
		Issuer: "https://test", Subject: name, DisplayName: name, Admin: true,
	}
}

func regularUser(name string) *faquser.User {
	return &faquser.User{
		Issuer: "https://test", Subject: name, DisplayName: name,
	}
}

func withResolver(u *faquser.User) func(*handler.Deps) {
	return func(d *handler.Deps) { d.Resolver = authedResolver{user: u} }
}

func TestRequireUserAnonReturns401(t *testing.T) {
	srv, _ := freshServer(t) // default resolver is anon
	rec := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodPost, "/probe", strings.NewReader(""))
	_, ok := handler.ExportRequireUser(srv, rec, rq)
	require.False(t, ok)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Header().Get("WWW-Authenticate"), "Bearer")
	require.Contains(t, rec.Header().Get("WWW-Authenticate"), "login_required")
}

func TestRequireUserAuthedReturnsUser(t *testing.T) {
	srv, _ := freshServer(t, withResolver(regularUser("alice")))
	rec := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodPost, "/probe", strings.NewReader(""))
	u, ok := handler.ExportRequireUser(srv, rec, rq)
	require.True(t, ok)
	require.NotNil(t, u)
	require.Equal(t, "alice", u.DisplayName)
}

func TestCanEditAsWithinGrace(t *testing.T) {
	srv, _ := freshServer(t)
	alice := regularUser("alice")
	aliceID := int64(1)
	now := time.Now()
	require.True(t, handler.ExportCanEditAs(srv, alice, aliceID, &aliceID, now, false),
		"author within grace should be allowed")
}

func TestCanEditAsAfterGrace(t *testing.T) {
	srv, _ := freshServer(t)
	alice := regularUser("alice")
	aliceID := int64(1)
	old := time.Now().Add(-time.Hour)
	require.False(t, handler.ExportCanEditAs(srv, alice, aliceID, &aliceID, old, false),
		"author after grace should be rejected")
}

func TestCanEditAsNonAuthorRejected(t *testing.T) {
	srv, _ := freshServer(t)
	eve := regularUser("eve")
	eveID := int64(2)
	aliceID := int64(1)
	require.False(t, handler.ExportCanEditAs(srv, eve, eveID, &aliceID, time.Now(), false),
		"non-author within grace should still be rejected")
}

func TestCanEditAsAdminAlwaysAllowed(t *testing.T) {
	srv, _ := freshServer(t)
	root := adminUser("root")
	old := time.Now().Add(-24 * time.Hour)
	aliceID := int64(1)
	require.True(t, handler.ExportCanEditAs(srv, root, 99, &aliceID, old, false),
		"admin overrides grace expiry")
	require.True(t, handler.ExportCanEditAs(srv, root, 99, &aliceID, old, true),
		"admin overrides locked parent too")
}

func TestCanEditAsLockedParentBlocksAuthor(t *testing.T) {
	srv, _ := freshServer(t)
	alice := regularUser("alice")
	aliceID := int64(1)
	require.False(t, handler.ExportCanEditAs(srv, alice, aliceID, &aliceID, time.Now(), true),
		"author within grace must still be blocked when the parent question is locked")
}

func TestCanAcceptQuestionAuthor(t *testing.T) {
	srv, _ := freshServer(t)
	alice := regularUser("alice")
	aliceID := int64(1)
	q := store.FaqQuestions{
		ID: 100, Status: store.FaqQuestionStatusOpen, AuthorUserID: &aliceID,
	}
	require.True(t, handler.ExportCanAccept(srv, alice, aliceID, q),
		"question author on open question should accept")
}

func TestCanAcceptNonAuthorRejected(t *testing.T) {
	srv, _ := freshServer(t)
	eve := regularUser("eve")
	eveID := int64(2)
	aliceID := int64(1)
	q := store.FaqQuestions{
		ID: 100, Status: store.FaqQuestionStatusOpen, AuthorUserID: &aliceID,
	}
	require.False(t, handler.ExportCanAccept(srv, eve, eveID, q))
}

func TestCanAcceptLockedAdminOnly(t *testing.T) {
	srv, _ := freshServer(t)
	root := adminUser("root")
	alice := regularUser("alice")
	aliceID := int64(1)
	q := store.FaqQuestions{
		ID: 100, Status: store.FaqQuestionStatusLocked, AuthorUserID: &aliceID,
	}
	require.True(t, handler.ExportCanAccept(srv, root, 99, q),
		"admin can accept on locked question")
	require.False(t, handler.ExportCanAccept(srv, alice, aliceID, q),
		"author cannot accept on locked question (admin-only per M4 decision)")
}

func TestCanCommentLockedAdminOnly(t *testing.T) {
	srv, _ := freshServer(t)
	alice := regularUser("alice")
	root := adminUser("root")
	require.False(t, handler.ExportCanComment(srv, nil, false), "anon rejected")
	require.True(t, handler.ExportCanComment(srv, alice, false), "authed on open")
	require.False(t, handler.ExportCanComment(srv, alice, true), "regular blocked on locked")
	require.True(t, handler.ExportCanComment(srv, root, true), "admin always permitted")
}

func TestResolveLocalUserCreatesRow(t *testing.T) {
	srv, _ := freshServer(t, withResolver(regularUser("alice")))
	row, err := handler.ExportResolveLocalUser(context.Background(), srv, regularUser("alice"))
	require.NoError(t, err)
	require.NotZero(t, row.ID)
	require.Equal(t, "alice", row.DisplayName)
	require.False(t, row.IsAdmin)
}

func TestResolveLocalUserSyncsAdminFlag(t *testing.T) {
	srv, _ := freshServer(t, withResolver(regularUser("alice")))

	// First time: regular user.
	row, err := handler.ExportResolveLocalUser(context.Background(), srv, regularUser("alice"))
	require.NoError(t, err)
	require.False(t, row.IsAdmin)

	// User is promoted upstream — same subject, now Admin=true.
	row2, err := handler.ExportResolveLocalUser(context.Background(), srv, adminUser("alice"))
	require.NoError(t, err)
	require.Equal(t, row.ID, row2.ID, "same sub_hash should map to same row")
	require.True(t, row2.IsAdmin, "admin flag should sync")
}

// Detail view should populate CanEdit/IsAdmin from the resolved user.
// Smoke test: author within grace gets CanEdit=true; non-author doesn't.
// The current detail template doesn't render an explicit Edit button
// yet (lands in M4.4), so we observe CanEdit indirectly by computing
// it through the helper after seeding a question.
func TestDetailViewCanEditAuthor(t *testing.T) {
	alice := regularUser("alice")
	srv, st := freshServer(t, withResolver(alice))
	q := seedQuestion(t, st, "alice", "Q", "body")

	// Look up alice's local id.
	aliceRow, err := handler.ExportResolveLocalUser(context.Background(), srv, alice)
	require.NoError(t, err)
	require.True(t, handler.ExportCanEditAs(srv, alice, aliceRow.ID, q.AuthorUserID, time.Now(), false),
		"alice as author should pass the gate")

	// A different user (eve) shouldn't.
	eve := regularUser("eve")
	eveRow, err := handler.ExportResolveLocalUser(context.Background(), srv, eve)
	require.NoError(t, err)
	require.False(t, handler.ExportCanEditAs(srv, eve, eveRow.ID, q.AuthorUserID, time.Now(), false),
		"eve as non-author should fail the gate")
}
