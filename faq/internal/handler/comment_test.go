package handler_test

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// directSeedComment inserts a comment on the given question authored by
// authorName, bypassing the HTTP handler so tests can prepare state
// owned by a user other than the request-time resolver's identity.
func directSeedComment(t *testing.T, qid int64, authorName, body string) int64 {
	t.Helper()
	h := sha256.Sum256([]byte("https://test:" + authorName))
	var uid int64
	require.NoError(t, testPool.QueryRow(context.Background(),
		`INSERT INTO faq_users (sub_hash, display_name)
		 VALUES ($1, $2)
		 ON CONFLICT (sub_hash) DO UPDATE SET display_name = EXCLUDED.display_name
		 RETURNING id`,
		h[:], authorName,
	).Scan(&uid))

	var cid int64
	require.NoError(t, testPool.QueryRow(context.Background(),
		`INSERT INTO faq_comments (question_id, body_md, body_html, author_user_id)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		qid, body, "<p>"+body+"</p>", uid,
	).Scan(&cid))
	return cid
}

func TestQuestionCommentHappyPath(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Q", "body")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/comments", url.Values{"body": {"a comment"}}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "a comment")

	var n int
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM faq_comments WHERE question_id = $1`, q.ID).Scan(&n))
	require.Equal(t, 1, n)
}

func TestAnswerCommentHappyPath(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	a := seedAnswer(t, st, q, "bob", "answer")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/a/"+itoa(a.ID)+"/comments", url.Values{"body": {"nice"}}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "nice")

	var n int
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM faq_comments WHERE answer_id = $1`, a.ID).Scan(&n))
	require.Equal(t, 1, n)
}

func TestCommentAnonRejected(t *testing.T) {
	srv, st := freshServer(t) // anon
	q := seedQuestion(t, st, "alice", "Q", "body")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/comments", url.Values{"body": {"x"}}))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCommentEmptyRejected(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/comments", url.Values{"body": {""}}))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestCommentTooLongRejected(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/comments",
		url.Values{"body": {strings.Repeat("x", 601)}}))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "600 characters")
}

func TestCommentOnLockedQuestionRegularRejected(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("bob")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_questions SET status='locked' WHERE id=$1`, q.ID)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/comments", url.Values{"body": {"x"}}))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCommentOnLockedQuestionAdminAllowed(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_questions SET status='locked' WHERE id=$1`, q.ID)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q/"+itoa(q.ID)+"/comments", url.Values{"body": {"admin note"}}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "admin note")
}

func TestEditCommentHappyPath(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	cid := directSeedComment(t, q.ID, "alice", "original")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, patchForm("/comments/"+itoa(cid), url.Values{"body": {"edited"}}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "edited")

	var bodyMD string
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT body_md FROM faq_comments WHERE id = $1`, cid).Scan(&bodyMD))
	require.Equal(t, "edited", bodyMD)
}

func TestEditCommentNonAuthorForbidden(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("eve")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	cid := directSeedComment(t, q.ID, "alice", "owned by alice")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, patchForm("/comments/"+itoa(cid), url.Values{"body": {"eve was here"}}))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestEditCommentAdminAfterGrace(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	cid := directSeedComment(t, q.ID, "alice", "old comment")
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_comments SET created_at = now() - interval '1 hour' WHERE id = $1`, cid)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, patchForm("/comments/"+itoa(cid), url.Values{"body": {"admin override"}}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "admin override")
}
