//go:build integration

package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

// nextSourcePostID is a monotonically increasing counter used by the
// imported-seed helpers so two seedImportedQuestion calls don't collide
// on the (source_id, source_post_id) unique constraint when they land
// in the same nanosecond.
var nextSourcePostID atomic.Int64

// seedImportedQuestion creates a 'locked' question marked needs_review
// with an explicit source, mirroring what the M2 importer produces.
// flagged controls whether needs_review is set on the question itself.
func seedImportedQuestion(t *testing.T, st *store.Store, src store.FaqSources, title string, flagged bool) store.FaqQuestions {
	t.Helper()
	ctx := context.Background()
	pid := nextSourcePostID.Add(1)
	q, err := st.CreateImportedQuestion(ctx, store.CreateImportedQuestionParams{
		Title:        title,
		BodyMd:       "body",
		BodyHtml:     "<p>body</p>",
		SourceID:     &src.ID,
		SourcePostID: &pid,
		SourceUrl:    strPtr("https://" + src.SourceDomain + "/q/" + itoa(pid)),
		NeedsReview:  flagged,
		CreatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)
	return q
}

// seedImportedAnswer attaches an imported answer to q with the given
// needs_review flag.
func seedImportedAnswer(t *testing.T, st *store.Store, q store.FaqQuestions, src store.FaqSources, flagged bool) store.FaqAnswers {
	t.Helper()
	ctx := context.Background()
	pid := nextSourcePostID.Add(1)
	a, err := st.CreateImportedAnswer(ctx, store.CreateImportedAnswerParams{
		QuestionID:   q.ID,
		BodyMd:       "answer body",
		BodyHtml:     "<p>answer body</p>",
		SourceID:     &src.ID,
		SourcePostID: &pid,
		SourceUrl:    strPtr("https://" + src.SourceDomain + "/a/" + itoa(pid)),
		NeedsReview:  flagged,
		CreatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)
	return a
}

func seedSource(t *testing.T, st *store.Store, domain, name string) store.FaqSources {
	t.Helper()
	src, err := st.UpsertSource(context.Background(), store.UpsertSourceParams{
		SourceDomain: domain,
		DisplayName:  name,
		License:      "CC BY-SA 4.0",
		DumpDate:     pgtype.Date{Time: pgDate(2023, time.December, 31), Valid: true},
	})
	require.NoError(t, err)
	return src
}

func strPtr(s string) *string { return &s }

func TestAdminReviewQueueAnonReturns401(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/review", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAdminReviewQueueNonAdminReturns403(t *testing.T) {
	srv, _ := freshServer(t, withResolver(regularUser("alice")))
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/review", nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAdminReviewQueueSurfacesFlaggedQuestion(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	src := seedSource(t, st, "stackoverflow.com", "Stack Overflow")
	seedImportedQuestion(t, st, src, "Flagged Question", true)
	seedImportedQuestion(t, st, src, "Unflagged", false)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/review", nil))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "Flagged Question")
	require.NotContains(t, rec.Body.String(), "Unflagged")
}

func TestAdminReviewQueueSurfacesQuestionWithFlaggedAnswer(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	src := seedSource(t, st, "stackoverflow.com", "Stack Overflow")
	q := seedImportedQuestion(t, st, src, "Clean Q With Dirty A", false)
	seedImportedAnswer(t, st, q, src, true)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/review", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Clean Q With Dirty A",
		"queue must surface questions whose answers are flagged even when the question itself is clean")
}

func TestAdminReviewQueueFiltersBySource(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	srcSO := seedSource(t, st, "stackoverflow.com", "Stack Overflow")
	srcRPG := seedSource(t, st, "qa.example.com", "RPG SE")
	seedImportedQuestion(t, st, srcSO, "SO Question", true)
	seedImportedQuestion(t, st, srcRPG, "RPG Question", true)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/review?source="+itoa(srcRPG.ID), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "RPG Question")
	require.NotContains(t, rec.Body.String(), "SO Question")
}

func TestAdminMarkReviewedClearsCascade(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	src := seedSource(t, st, "stackoverflow.com", "Stack Overflow")
	q := seedImportedQuestion(t, st, src, "Both flagged", true)
	a := seedImportedAnswer(t, st, q, src, true)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm(
		"/admin/review/"+itoa(q.ID)+"/reviewed", url.Values{"source": {""}}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var qFlag, aFlag bool
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT needs_review FROM faq_questions WHERE id = $1`, q.ID).Scan(&qFlag))
	require.False(t, qFlag, "question's needs_review must be cleared")
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT needs_review FROM faq_answers WHERE id = $1`, a.ID).Scan(&aFlag))
	require.False(t, aFlag, "cascade must clear answers' needs_review too")
}

func TestAdminDeletedListsDeletedRows(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Will be deleted", "body")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/delete", nil))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/deleted", nil))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "Will be deleted")
}

func TestAdminDeletedAnonReturns401(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/deleted", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
