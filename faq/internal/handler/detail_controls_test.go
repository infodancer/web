package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/internal/handler"
)

// withGrace sets the edit grace window so an author's own posts show Edit.
func withGrace(d time.Duration) func(*handler.Deps) {
	return func(dep *handler.Deps) { dep.EditGracePeriod = d }
}

// answerDeletedAt reads an answer's deleted_at (nil = live).
func answerDeletedAt(t *testing.T, id int64) *time.Time {
	t.Helper()
	var at *time.Time
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT deleted_at FROM faq_answers WHERE id=$1`, id).Scan(&at))
	return at
}

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

// The answer body, footer ("answered by …"), and comments live in the wide
// column wrapper, not the narrow stats column.
func TestDetailControls_AnswerFooterInWideColumn(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("dave")))
	q := seedQuestion(t, st, "alice", "Open Q", "body")
	seedAnswer(t, st, q, "bob", "bob's answer")

	body := getDetailBody(t, srv, q.ID)
	require.Contains(t, body, "faq-answer-main", "body/footer share the wide column wrapper")
	require.Contains(t, body, "answered by")
}

// An author within the grace window sees Edit + Delete on their own answer.
func TestDetailControls_AuthorSeesEditAndDelete(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")), withGrace(time.Hour))
	q := seedQuestion(t, st, "alice", "Open Q", "body")
	a := seedAnswer(t, st, q, "bob", "bob's answer")

	body := getDetailBody(t, srv, q.ID)
	require.Contains(t, body, `hx-get="../../a/`+itoa(a.ID)+`/edit"`, "author sees Edit")
	require.Contains(t, body, `action="../../a/`+itoa(a.ID)+`/delete"`, "author sees Delete")
}

// A non-author non-admin gets neither edit nor delete on someone else's answer.
func TestDetailControls_NonAuthorNoEditOrDelete(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("eve")), withGrace(time.Hour))
	q := seedQuestion(t, st, "alice", "Open Q", "body")
	a := seedAnswer(t, st, q, "bob", "bob's answer")

	body := getDetailBody(t, srv, q.ID)
	require.NotContains(t, body, `/a/`+itoa(a.ID)+`/edit`)
	require.NotContains(t, body, `/a/`+itoa(a.ID)+`/delete`)
}

// Deleting your own answer soft-deletes it and hides it from a non-admin view.
func TestDetailControls_AuthorDeletesOwnAnswer(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Open Q", "body")
	a := seedAnswer(t, st, q, "bob", "bobs-unique-answer-text")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/a/"+itoa(a.ID)+"/delete", url.Values{}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.NotNil(t, answerDeletedAt(t, a.ID), "deleted_at set")

	body := getDetailBody(t, srv, q.ID)
	require.NotContains(t, body, "bobs-unique-answer-text", "hidden from the non-admin author after delete")
}

// A non-author non-admin cannot delete an answer (403, stays live).
func TestDetailControls_DeleteForbiddenForNonAuthor(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("eve")))
	q := seedQuestion(t, st, "alice", "Open Q", "body")
	a := seedAnswer(t, st, q, "bob", "answer")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/a/"+itoa(a.ID)+"/delete", url.Values{}))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Nil(t, answerDeletedAt(t, a.ID), "answer stays live")
}

// An admin still sees a soft-deleted answer (muted, with Restore) and can restore it.
func TestDetailControls_AdminSeesAndRestoresDeleted(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Open Q", "body")
	a := seedAnswer(t, st, q, "bob", "bobs-unique-answer-text")
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_answers SET deleted_at=now() WHERE id=$1`, a.ID)
	require.NoError(t, err)

	body := getDetailBody(t, srv, q.ID)
	require.Contains(t, body, "bobs-unique-answer-text", "admin still sees the deleted answer")
	require.Contains(t, body, "faq-deleted-note")
	require.Contains(t, body, `action="../../a/`+itoa(a.ID)+`/restore"`, "admin sees Restore")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/a/"+itoa(a.ID)+"/restore", url.Values{}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Nil(t, answerDeletedAt(t, a.ID), "restored")
}

// Deleting the accepted answer clears the question's accepted_answer_id.
func TestDetailControls_DeletingAcceptedClearsAccepted(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Open Q", "body")
	a := seedAnswer(t, st, q, "bob", "answer")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/accept", url.Values{"answer_id": {itoa(a.ID)}}))
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/a/"+itoa(a.ID)+"/delete", url.Values{}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var acc *int64
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT accepted_answer_id FROM faq_questions WHERE id=$1`, q.ID).Scan(&acc))
	require.Nil(t, acc, "accepted_answer_id cleared when the accepted answer is deleted")
}

// The question author sees an Edit control in the action bar.
func TestDetailControls_QuestionAuthorSeesEdit(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")), withGrace(time.Hour))
	q := seedQuestion(t, st, "alice", "Open Q", "body")

	body := getDetailBody(t, srv, q.ID)
	require.Contains(t, body, `hx-get="../../q/`+itoa(q.ID)+`/edit"`, "question author sees Edit")
}
