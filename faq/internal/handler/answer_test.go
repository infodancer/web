package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostAnswerHappyPath(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Q", "body")

	form := url.Values{"body": {"a real answer"}}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/answers", form))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "a real answer")

	var nA int
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM faq_answers WHERE question_id = $1`, q.ID).Scan(&nA))
	require.Equal(t, 1, nA)
}

func TestPostAnswerAnonRejected(t *testing.T) {
	srv, st := freshServer(t) // anon
	q := seedQuestion(t, st, "alice", "Q", "body")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/answers", url.Values{"body": {"x"}}))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestPostAnswerOnLockedQuestionRejected(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_questions SET status='locked' WHERE id=$1`, q.ID)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/answers", url.Values{"body": {"x"}}))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "locked")
}

func TestPostAnswerEmptyBodyRejected(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Q", "body")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/answers", url.Values{"body": {""}}))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "body is required")
}

func TestEditAnswerForm(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "original answer")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/a/"+itoa(a.ID)+"/edit", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "original answer")
	require.NotContains(t, rec.Body.String(), "<!DOCTYPE")
}

func TestEditAnswerFormNonAuthorForbidden(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("eve")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "answer")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/a/"+itoa(a.ID)+"/edit", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestEditAnswerSubmitHappyPath(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "original")

	form := url.Values{"body": {"edited **answer**"}}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, patchForm("/a/"+itoa(a.ID), form))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	body := rec.Body.String()
	require.Contains(t, body, "<strong>answer</strong>")

	var lm bool
	var ec int
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT locally_modified, edit_count FROM faq_answers WHERE id = $1`, a.ID).Scan(&lm, &ec))
	require.True(t, lm)
	require.Equal(t, 1, ec)
}

func TestEditAnswerSubmitGraceExpired(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "original")
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_answers SET created_at = now() - interval '1 hour' WHERE id = $1`, a.ID)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, patchForm("/a/"+itoa(a.ID), url.Values{"body": {"too late"}}))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestEditAnswerSubmitAdminAfterGrace(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "original")
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_answers SET created_at = now() - interval '1 hour' WHERE id = $1`, a.ID)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, patchForm("/a/"+itoa(a.ID), url.Values{"body": {"admin override"}}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "admin override")
}
