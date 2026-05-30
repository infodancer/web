//go:build integration

package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// deleteForm constructs a same-origin DELETE with no body (vote
// removal doesn't need form fields).
func deleteForm(target string) *http.Request {
	r := httptest.NewRequest(http.MethodDelete, target, strings.NewReader(""))
	r.Host = "example.com"
	r.Header.Set("Origin", "https://example.com")
	return r
}

func TestVoteUpHappyPath(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "alice", "answer")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/a/"+itoa(a.ID)+"/vote", url.Values{"value": {"up"}}))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "faq-vote-")
	require.Contains(t, rec.Body.String(), `class="faq-vote-up is-active"`)
	require.Contains(t, rec.Body.String(), ">1<") // score is 1

	var score int
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT local_score FROM faq_answers WHERE id = $1`, a.ID).Scan(&score))
	require.Equal(t, 1, score)
}

func TestVoteDownAndFlip(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "alice", "answer")

	// Down first.
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/a/"+itoa(a.ID)+"/vote", url.Values{"value": {"down"}}))
	require.Equal(t, http.StatusOK, rec.Code)
	var score int
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT local_score FROM faq_answers WHERE id = $1`, a.ID).Scan(&score))
	require.Equal(t, -1, score)

	// Flip to up: trigger delta is +2.
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/a/"+itoa(a.ID)+"/vote", url.Values{"value": {"up"}}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT local_score FROM faq_answers WHERE id = $1`, a.ID).Scan(&score))
	require.Equal(t, 1, score)
}

func TestVoteRemovedByDelete(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "alice", "answer")

	// Up vote, then delete.
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/a/"+itoa(a.ID)+"/vote", url.Values{"value": {"up"}}))
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, deleteForm("/a/"+itoa(a.ID)+"/vote"))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var score int
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT local_score FROM faq_answers WHERE id = $1`, a.ID).Scan(&score))
	require.Equal(t, 0, score)
}

func TestVoteSelfRejected(t *testing.T) {
	// alice tries to vote on her own answer.
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "bob", "Q", "body")
	a := seedAnswer(t, st, q, "alice", "answer")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/a/"+itoa(a.ID)+"/vote", url.Values{"value": {"up"}}))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "own answer")
}

func TestVoteLockedQuestionRejected(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "alice", "answer")
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_questions SET status='locked' WHERE id=$1`, q.ID)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/a/"+itoa(a.ID)+"/vote", url.Values{"value": {"up"}}))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "locked")
}

func TestVoteAnonRejected(t *testing.T) {
	srv, st := freshServer(t) // anon
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "alice", "answer")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/a/"+itoa(a.ID)+"/vote", url.Values{"value": {"up"}}))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestVoteBadValueRejected(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "alice", "answer")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/a/"+itoa(a.ID)+"/vote", url.Values{"value": {"sideways"}}))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
