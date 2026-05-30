//go:build integration

package handler_test

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

// seedQuestion creates one native question authored by name. Returns the
// faq_questions row. Tags is a slice of tag names to attach.
func seedQuestion(t *testing.T, st *store.Store, name, title, body string, tags ...string) store.FaqQuestions {
	t.Helper()
	ctx := context.Background()
	h := sha256.Sum256([]byte("https://test:" + name))
	u, err := st.UpsertUser(ctx, store.UpsertUserParams{
		SubHash: h[:], DisplayName: name,
	})
	require.NoError(t, err)
	q, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title: title, BodyMd: body, BodyHtml: "<p>" + body + "</p>",
		AuthorUserID: &u.ID,
	})
	require.NoError(t, err)
	for _, name := range tags {
		tag, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: name})
		require.NoError(t, err)
		require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
			QuestionID: q.ID, TagID: tag.ID,
		}))
	}
	return q
}

func seedAnswer(t *testing.T, st *store.Store, q store.FaqQuestions, name, body string) store.FaqAnswers {
	t.Helper()
	ctx := context.Background()
	h := sha256.Sum256([]byte("https://test:" + name))
	u, err := st.UpsertUser(ctx, store.UpsertUserParams{SubHash: h[:], DisplayName: name})
	require.NoError(t, err)
	a, err := st.CreateAnswer(ctx, store.CreateAnswerParams{
		QuestionID: q.ID, BodyMd: body, BodyHtml: "<p>" + body + "</p>",
		AuthorUserID: &u.ID,
	})
	require.NoError(t, err)
	return a
}

func TestQuestionsListEmpty(t *testing.T) {
	srv, _ := freshServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "No questions yet.")
	require.Contains(t, body, "<title>")
	require.Contains(t, body, "All questions")
}

func TestQuestionsListNewest(t *testing.T) {
	srv, st := freshServer(t)

	seedQuestion(t, st, "alice", "Older question", "first body", "tag-a")
	q2 := seedQuestion(t, st, "bob", "Newer question", "second body", "tag-a", "tag-b")
	seedAnswer(t, st, q2, "carol", "an answer")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	// Newest sort: "Newer question" must appear before "Older question".
	newerIdx := strings.Index(body, "Newer question")
	olderIdx := strings.Index(body, "Older question")
	require.GreaterOrEqual(t, newerIdx, 0, "Newer question missing from body")
	require.GreaterOrEqual(t, olderIdx, 0, "Older question missing from body")
	require.Less(t, newerIdx, olderIdx, "newest sort should put newer question first")

	// Tags rendered on the card.
	require.Contains(t, body, "tag-a")
	require.Contains(t, body, "tag-b")
}

func TestQuestionsListHTMXReturnsPartial(t *testing.T) {
	srv, st := freshServer(t)
	seedQuestion(t, st, "alice", "Q", "body")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q", nil)
	req.Header.Set("HX-Request", "true")
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	// Partial must NOT include the layout chrome.
	require.NotContains(t, body, "<!DOCTYPE")
	require.NotContains(t, body, "<title>")
	// But must include the question card.
	require.Contains(t, body, "Q")
}

func TestQuestionsListSortVotes(t *testing.T) {
	srv, st := freshServer(t)

	qLow := seedQuestion(t, st, "alice", "Low score", "body")
	qHigh := seedQuestion(t, st, "alice", "High score", "body")
	seedAnswer(t, st, qLow, "bob", "low ans")
	seedAnswer(t, st, qHigh, "bob", "high ans")

	// Manually bump qHigh's answer score to ensure deterministic order
	// without relying on the vote trigger (which needs faq_users / vote
	// rows).
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_answers SET local_score = 5 WHERE question_id = $1`, qHigh.ID)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q?sort=votes", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	high := strings.Index(body, "High score")
	low := strings.Index(body, "Low score")
	require.GreaterOrEqual(t, high, 0)
	require.GreaterOrEqual(t, low, 0)
	require.Less(t, high, low, "votes sort: high score should come first")
}

func TestQuestionsListSortUnanswered(t *testing.T) {
	srv, st := freshServer(t)

	seedQuestion(t, st, "alice", "Answered Q", "body")
	q2 := seedQuestion(t, st, "alice", "Unanswered Q", "body")
	_ = q2
	answered := seedQuestion(t, st, "alice", "Also answered", "body")
	seedAnswer(t, st, answered, "bob", "ans")
	seedAnswer(t, st, seedQuestion(t, st, "alice", "Hidden answered", "body"), "bob", "ans")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q?sort=unanswered", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Unanswered Q")
	require.NotContains(t, body, "Also answered")
	require.NotContains(t, body, "Hidden answered")
}

func TestQuestionsListImportedShowsAttribution(t *testing.T) {
	srv, _ := freshServer(t)
	ctx := context.Background()

	src, err := testPool.Exec(ctx, `
		INSERT INTO faq_sources (source_domain, display_name, license, dump_date)
		VALUES ('qa.example.com', 'Example Q&A', 'CC BY-SA 4.0', $1)
	`, pgDate(2023, 9, 1))
	require.NoError(t, err)
	_ = src

	var srcID int64
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT id FROM faq_sources WHERE source_domain = 'qa.example.com'`).Scan(&srcID))
	url := "https://qa.example.com/q/100"
	_, err = testPool.Exec(ctx, `
		INSERT INTO faq_questions (title, body_md, body_html, status, source_id, source_post_id, source_url)
		VALUES ('Imported Q', 'imp body', '<p>imp body</p>', 'locked', $1, 100, $2)
	`, srcID, url)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Imported Q")
	require.Contains(t, body, "Example Q&amp;A", "source display name is HTML-escaped (it is source-controlled)")
	require.Contains(t, body, "CC BY-SA 4.0")
	require.Contains(t, body, "Archived") // locked badge
	require.Contains(t, body, url)
}

func TestQuestionsListUnknownSort404(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q?sort=bogus", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// silence pgtype import (kept for future tests).
var _ = pgtype.Timestamptz{}
