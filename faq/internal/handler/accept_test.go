package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAcceptByAuthorOnOpen(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "answer")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/accept",
		url.Values{"answer_id": {itoa(a.ID)}}))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "Accepted")

	var acc *int64
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT accepted_answer_id FROM faq_questions WHERE id = $1`, q.ID).Scan(&acc))
	require.NotNil(t, acc)
	require.Equal(t, a.ID, *acc)
}

func TestAcceptToggleClears(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "answer")

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/accept",
			url.Values{"answer_id": {itoa(a.ID)}}))
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	}

	// After two toggles, accepted_answer_id is back to NULL.
	var acc *int64
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT accepted_answer_id FROM faq_questions WHERE id = $1`, q.ID).Scan(&acc))
	require.Nil(t, acc)
}

func TestAcceptByNonAuthorRejected(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("eve")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "answer")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/accept",
		url.Values{"answer_id": {itoa(a.ID)}}))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAcceptByAdminAllowedOnLocked(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "answer")
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_questions SET status='locked' WHERE id=$1`, q.ID)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/accept",
		url.Values{"answer_id": {itoa(a.ID)}}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

func TestAcceptByAuthorOnLockedRejected(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "answer")
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_questions SET status='locked' WHERE id=$1`, q.ID)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/accept",
		url.Values{"answer_id": {itoa(a.ID)}}))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAcceptAnonRejected(t *testing.T) {
	srv, st := freshServer(t) // anon
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "answer")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/accept",
		url.Values{"answer_id": {itoa(a.ID)}}))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAcceptAnswerFromAnotherQuestionRejected(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q1 := seedQuestion(t, st, "alice", "Q1", "body")
	q2 := seedQuestion(t, st, "alice", "Q2", "body")
	otherAnswer := seedAnswer(t, st, q2, "bob", "answer to q2")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q1.ID)+"/accept",
		url.Values{"answer_id": {itoa(otherAnswer.ID)}}))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "does not belong")
}

func TestAcceptBadAnswerIDRejected(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/accept",
		url.Values{"answer_id": {"abc"}}))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
