//go:build integration

package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

func TestQuestionDetailHappyPath(t *testing.T) {
	srv, st := freshServer(t)

	q := seedQuestion(t, st, "alice", "How does counterspell work?", "I want to know.", "dnd-5e", "spells")
	a1 := seedAnswer(t, st, q, "bob", "answer one")
	seedAnswer(t, st, q, "carol", "answer two")

	// Mark a1 accepted.
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_questions SET accepted_answer_id = $1 WHERE id = $2`, a1.ID, q.ID)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q/1/how-does-counterspell-work", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "How does counterspell work?")
	require.Contains(t, body, "<p>I want to know.</p>") // body_html, safe pipe
	require.Contains(t, body, "answer one")
	require.Contains(t, body, "answer two")
	require.Contains(t, body, "alice")
	require.Contains(t, body, "bob")
	require.Contains(t, body, "Accepted")
	require.Contains(t, body, "dnd-5e")
}

// TestGlobalDetail_CanonicalURL exercises the canonical-URL emission
// for /q/{id} per D8.3 (revised): without a canonical_subsite_id the
// global URL is canonical; with one set, the subsite URL wins.
func TestGlobalDetail_CanonicalURL(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	// Case A: no subsites — canonical = global URL.
	q := seedQuestion(t, st, "alice", "Plain question", "body")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/q/"+itoa(q.ID)+"/plain-question", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(),
		`<link rel="canonical" href="/faq/q/`+itoa(q.ID))

	// Case B: a subsite owns the question (native subsite_id set) —
	// canonical = subsite URL.
	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "bs", DisplayName: "BS",
	})
	require.NoError(t, err)
	bob := mustHandlerUser(t, st, "bob")
	native, err := st.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title: "Native one", BodyMd: "b", BodyHtml: "<p>b</p>",
		AuthorUserID: &bob.ID, SubsiteID: &sub.ID,
	})
	require.NoError(t, err)
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/q/"+itoa(native.ID)+"/native-one", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(),
		`<link rel="canonical" href="/faq/bs/q/`+itoa(native.ID),
		"native question must canonicalize to its subsite even from the global URL")
}

func TestQuestionDetailSlugRedirect(t *testing.T) {
	srv, st := freshServer(t)
	q := seedQuestion(t, st, "alice", "How does counterspell work?", "body")

	rec := httptest.NewRecorder()
	url := "/q/" + itoa(q.ID) + "/wrong-slug"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusMovedPermanently, rec.Code)
	loc := rec.Header().Get("Location")
	require.Contains(t, loc, "/faq/q/")
	require.Contains(t, loc, "how-does-counterspell-work")
}

func TestQuestionDetailNoSlugDoesNotRedirect(t *testing.T) {
	srv, st := freshServer(t)
	q := seedQuestion(t, st, "alice", "Title", "body")

	rec := httptest.NewRecorder()
	url := "/q/" + itoa(q.ID)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestQuestionDetailMissing404(t *testing.T) {
	srv, _ := freshServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q/999/whatever", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestQuestionDetailDeletedHidden(t *testing.T) {
	srv, st := freshServer(t)
	q := seedQuestion(t, st, "alice", "Soft-deleted", "body")
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_questions SET status = 'deleted' WHERE id = $1`, q.ID)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q/"+itoa(q.ID)+"/soft-deleted", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestQuestionDetailImportedAttribution(t *testing.T) {
	srv, _ := freshServer(t)
	ctx := context.Background()

	_, err := testPool.Exec(ctx, `
		INSERT INTO faq_sources (source_domain, display_name, license, dump_date)
		VALUES ('qa.example.com', 'Example Q&A', 'CC BY-SA 4.0', $1)
	`, pgDate(2023, 9, 1))
	require.NoError(t, err)

	var srcID int64
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT id FROM faq_sources WHERE source_domain = 'qa.example.com'`).Scan(&srcID))

	url := "https://qa.example.com/q/42"
	var qID int64
	require.NoError(t, testPool.QueryRow(ctx, `
		INSERT INTO faq_questions (title, body_md, body_html, status, source_id, source_post_id, source_url)
		VALUES ('Imported Q', 'imp body', '<p>imp body</p>', 'locked', $1, 42, $2)
		RETURNING id
	`, srcID, url).Scan(&qID))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q/"+itoa(qID)+"/imported-q", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Imported Q")
	require.Contains(t, body, "Archived") // locked badge
	require.Contains(t, body, "Example Q&amp;A", "source display name is HTML-escaped (it is source-controlled)")
	require.Contains(t, body, "CC BY-SA 4.0")
	require.Contains(t, body, url)
}

func TestQuestionDetailHTMXReturnsPartial(t *testing.T) {
	srv, st := freshServer(t)
	q := seedQuestion(t, st, "alice", "Q", "body")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q/"+itoa(q.ID), nil)
	req.Header.Set("HX-Request", "true")
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.NotContains(t, body, "<!DOCTYPE")
	require.NotContains(t, body, "<title>")
	require.Contains(t, body, "<article class=\"faq-q\"")
}

func TestQuestionDetailQuestionCommentRendered(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	q := seedQuestion(t, st, "alice", "Q", "body")
	_, err := st.CreateQuestionComment(ctx, store.CreateQuestionCommentParams{
		QuestionID: &q.ID,
		BodyMd:     "great q",
		BodyHtml:   "<em>great q</em>",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q/"+itoa(q.ID), nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "<em>great q</em>")
}

func TestQuestionDetailBadID404(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q/abc/anything", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func itoa(n int64) string {
	// Avoids the strconv import; itoa is exclusive to this test file.
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b strings.Builder
	for n > 0 {
		b.WriteByte(digits[n%10])
		n /= 10
	}
	out := []byte(b.String())
	// reverse
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
