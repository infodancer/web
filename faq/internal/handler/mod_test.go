package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

// M5 moderation tests.

func TestLockQuestionByAdmin(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Q", "body")

	rec := httptest.NewRecorder()
	rq := postForm("/q/"+itoa(q.ID)+"/lock", url.Values{"reason": {"spam"}})
	rq.Header.Set("HX-Request", "true")
	srv.Routes().ServeHTTP(rec, rq)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var status string
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT status FROM faq_questions WHERE id = $1`, q.ID).Scan(&status))
	require.Equal(t, "locked", status)
}

func TestLockQuestionByNonAdminRejected(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Q", "body")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/lock", nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestLockQuestionAnonReturns401(t *testing.T) {
	srv, st := freshServer(t) // anon resolver
	q := seedQuestion(t, st, "alice", "Q", "body")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/lock", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Header().Get("WWW-Authenticate"), "Bearer")
}

func TestUnlockQuestionRoundTrip(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Q", "body")

	// Lock first.
	rec := httptest.NewRecorder()
	rq := postForm("/q/"+itoa(q.ID)+"/lock", nil)
	rq.Header.Set("HX-Request", "true")
	srv.Routes().ServeHTTP(rec, rq)
	require.Equal(t, http.StatusOK, rec.Code)

	// Then unlock.
	rec = httptest.NewRecorder()
	rq = postForm("/q/"+itoa(q.ID)+"/unlock", nil)
	rq.Header.Set("HX-Request", "true")
	srv.Routes().ServeHTTP(rec, rq)
	require.Equal(t, http.StatusOK, rec.Code)

	var status string
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT status FROM faq_questions WHERE id = $1`, q.ID).Scan(&status))
	require.Equal(t, "open", status)
}

func TestDeleteQuestionPreservesPrevStatus(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Q", "body")

	// Lock the question first so prev_status round-trips through 'locked'.
	require.NoError(t, st.SetQuestionStatus(context.Background(), store.SetQuestionStatusParams{
		ID: q.ID, Status: store.FaqQuestionStatusLocked,
	}))

	// Now delete.
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/delete", url.Values{"reason": {"removed"}}))
	require.Equal(t, http.StatusSeeOther, rec.Code, "body: %s", rec.Body.String())

	var status string
	var prev *string
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT status, prev_status FROM faq_questions WHERE id = $1`, q.ID).Scan(&status, &prev))
	require.Equal(t, "deleted", status)
	require.NotNil(t, prev)
	require.Equal(t, "locked", *prev)
}

func TestRestoreQuestionRestoresLocked(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Q", "body")

	// Lock → delete → restore should land back at 'locked'.
	ctx := context.Background()
	require.NoError(t, st.SetQuestionStatus(ctx, store.SetQuestionStatusParams{
		ID: q.ID, Status: store.FaqQuestionStatusLocked,
	}))

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/delete", nil))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/restore", nil))
	require.Equal(t, http.StatusSeeOther, rec.Code, "body: %s", rec.Body.String())

	var status string
	var prev *string
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT status, prev_status FROM faq_questions WHERE id = $1`, q.ID).Scan(&status, &prev))
	require.Equal(t, "locked", status, "restored row should round-trip back to locked")
	require.Nil(t, prev, "prev_status should clear on restore")
}

func TestRestoreQuestionRestoresOpen(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	ctx := context.Background()

	// Delete an open question → restore → status returns to 'open'.
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/delete", nil))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/restore", nil))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	var status string
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT status FROM faq_questions WHERE id = $1`, q.ID).Scan(&status))
	require.Equal(t, "open", status)
}

func TestDeleteQuestionHidesFromList(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Deleted Q", "body")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/delete", nil))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	// Now list view should not show it.
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/q", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "Deleted Q")
}

// Locked-parent edits: a non-admin author of an answer/comment must be
// blocked once the parent question is locked, but admins still edit.
func TestAnswerEditBlockedByLockedParent(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "answer body")

	// Lock the question directly via the store so we exercise the gate.
	require.NoError(t, st.SetQuestionStatus(context.Background(), store.SetQuestionStatusParams{
		ID: q.ID, Status: store.FaqQuestionStatusLocked,
	}))

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, patchForm("/a/"+itoa(a.ID), url.Values{"body": {"revised"}}))
	require.Equal(t, http.StatusForbidden, rec.Code,
		"non-admin author should not edit answer under locked question")
}

func TestAnswerEditAllowedForAdminOnLocked(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "answer body")
	require.NoError(t, st.SetQuestionStatus(context.Background(), store.SetQuestionStatusParams{
		ID: q.ID, Status: store.FaqQuestionStatusLocked,
	}))

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, patchForm("/a/"+itoa(a.ID), url.Values{"body": {"admin edit"}}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

func TestQuestionEditBlockedByLockedSelf(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	require.NoError(t, st.SetQuestionStatus(context.Background(), store.SetQuestionStatusParams{
		ID: q.ID, Status: store.FaqQuestionStatusLocked,
	}))

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, patchForm("/q/"+itoa(q.ID),
		url.Values{"title": {"new title"}, "body": {"new body"}, "tags": {"t"}}))
	require.Equal(t, http.StatusForbidden, rec.Code,
		"author should not edit their own question once it is locked")
}
