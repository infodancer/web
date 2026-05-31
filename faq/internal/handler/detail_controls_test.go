//go:build integration

package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/internal/handler"
)

// withSignInURL sets Deps.SignInURL so the anonymous answer prompt links to it.
func withSignInURL(url string) func(*handler.Deps) {
	return func(d *handler.Deps) { d.SignInURL = url }
}

// getDetailBody GETs the (no-slug) detail URL and returns the 200 body.
func getDetailBody(t *testing.T, srv *handler.Server, id int64) string {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/q/"+itoa(id), nil))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	return rec.Body.String()
}

// A signed-in viewer who is neither the asker nor an answer's author gets the
// answer form and an interactive vote cell on each answer, but no accept toggle.
func TestDetailControls_AuthedNonOwnerSeesFormAndVotes(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("dave")))
	q := seedQuestion(t, st, "alice", "Open Q", "body")
	seedAnswer(t, st, q, "bob", "bob's answer")

	body := getDetailBody(t, srv, q.ID)
	require.Contains(t, body, "faq-form-answer", "answer form present for a signed-in viewer")
	require.Contains(t, body, `name="body"`)
	require.Contains(t, body, "faq-vote-up", "vote cell present on another user's answer")
	require.NotContains(t, body, "faq-accept-btn", "a non-owner sees no accept control")
}

// Anonymous visitors see a sign-in prompt (linked when SignInURL is set) and a
// read-only score — no answer form, no vote buttons.
func TestDetailControls_AnonymousSeesSignInPrompt(t *testing.T) {
	srv, st := freshServer(t, withSignInURL("/login"))
	q := seedQuestion(t, st, "alice", "Open Q", "body")
	seedAnswer(t, st, q, "bob", "bob's answer")

	body := getDetailBody(t, srv, q.ID)
	require.Contains(t, body, "faq-signin-prompt")
	require.Contains(t, body, `href="/login"`)
	require.Contains(t, body, "to post an answer")
	require.NotContains(t, body, `name="body"`, "no answer form for anonymous")
	require.NotContains(t, body, "faq-vote-up", "anonymous gets a read-only score, no vote buttons")
	require.Contains(t, body, "faq-answer-score", "read-only score still shown")
}

// The question owner gets the accept toggle on answers (and can still answer).
func TestDetailControls_OwnerSeesAccept(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Open Q", "body")
	seedAnswer(t, st, q, "bob", "bob's answer")

	body := getDetailBody(t, srv, q.ID)
	require.Contains(t, body, "faq-accept-btn", "the question owner sees the accept control")
	require.Contains(t, body, `name="answer_id"`)
	require.Contains(t, body, `name="body"`, "the owner can also answer")
}

// A viewer never gets vote buttons on their own answer (the score trigger would
// reject the vote), but can still post a new answer.
func TestDetailControls_NoVoteOnOwnAnswer(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Open Q", "body")
	seedAnswer(t, st, q, "bob", "bob's own answer")

	body := getDetailBody(t, srv, q.ID)
	require.NotContains(t, body, "faq-vote-up", "no vote buttons on the viewer's own answer")
	require.Contains(t, body, "faq-answer-score", "own answer shows a read-only score")
	require.Contains(t, body, `name="body"`, "bob can still answer an open question")
}

// A locked question freezes the write surface: no answer form, no sign-in
// prompt, no voting, and no accept for a non-admin.
func TestDetailControls_LockedHidesWriteControls(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("dave")))
	q := seedQuestion(t, st, "alice", "Locked Q", "body")
	seedAnswer(t, st, q, "bob", "bob's answer")
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_questions SET status='locked' WHERE id=$1`, q.ID)
	require.NoError(t, err)

	body := getDetailBody(t, srv, q.ID)
	require.NotContains(t, body, `name="body"`, "no answer form on a locked question")
	require.NotContains(t, body, "faq-signin-prompt", "no sign-in prompt on a locked question")
	require.NotContains(t, body, "faq-vote-up", "no voting on a locked question")
	require.NotContains(t, body, "faq-accept-btn", "a non-admin sees no accept on a locked question")
}
