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

// postForm constructs a same-origin POST to target with form-encoded
// values. Picks up the host's CSRF guard automatically.
func postForm(target string, vals url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(vals.Encode()))
	r.Host = "example.com"
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func patchForm(target string, vals url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPatch, target, strings.NewReader(vals.Encode()))
	r.Host = "example.com"
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestAskGetForm(t *testing.T) {
	srv, _ := freshServer(t, withResolver(regularUser("alice")))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q/new", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Ask a question")
	require.Contains(t, rec.Body.String(), `name="title"`)
}

func TestAskGetFormAnonRequiresAuth(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q/new", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Header().Get("WWW-Authenticate"), "Bearer")
}

func TestAskSubmitHappyPath(t *testing.T) {
	srv, _ := freshServer(t, withResolver(regularUser("alice")))
	form := url.Values{
		"title": {"How does counterspell work?"},
		"body":  {"I'd like to know **how** it works."},
		"tags":  {"dnd-5e, spells"},
	}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q", form))
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Contains(t, rec.Header().Get("Location"), "/faq/q/")
	require.Contains(t, rec.Header().Get("Location"), "how-does-counterspell-work")
}

func TestAskSubmitHTMXRedirectsViaHeader(t *testing.T) {
	srv, _ := freshServer(t, withResolver(regularUser("alice")))
	form := url.Values{
		"title": {"htmx redirect"},
		"body":  {"x"},
	}
	req := postForm("/q", form)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("HX-Redirect"), "/faq/q/")
}

func TestAskSubmitValidationErrors(t *testing.T) {
	srv, _ := freshServer(t, withResolver(regularUser("alice")))
	cases := []struct {
		name string
		vals url.Values
		want string
	}{
		{"empty-title", url.Values{"title": {""}, "body": {"x"}}, "title is required"},
		{"empty-body", url.Values{"title": {"q"}, "body": {""}}, "body is required"},
		{"long-title", url.Values{"title": {strings.Repeat("x", 151)}, "body": {"x"}}, "title must be"},
		{"long-body", url.Values{"title": {"q"}, "body": {strings.Repeat("y", 30_001)}}, "body must be"},
		{"too-many-tags", url.Values{"title": {"q"}, "body": {"b"}, "tags": {"a,b,c,d,e,f"}}, "at most 5 tags"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rec, postForm("/q", tc.vals))
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			require.Contains(t, rec.Body.String(), tc.want)
		})
	}
}

func TestAskSubmitAnonRejected(t *testing.T) {
	srv, _ := freshServer(t) // anon
	form := url.Values{"title": {"x"}, "body": {"y"}}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q", form))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAskSubmitNormalizesAndDeduplicatesTags(t *testing.T) {
	srv, _ := freshServer(t, withResolver(regularUser("alice")))
	// Each entry normalizes to "dnd-5e"; world-building stays distinct.
	// Both entries should collapse to one attachment.
	form := url.Values{
		"title": {"a tag test"},
		"body":  {"b"},
		"tags":  {"DND-5E, dnd-5e, world-building"},
	}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/q", form))
	require.Equal(t, http.StatusSeeOther, rec.Code, "body: %s", rec.Body.String())

	var n int
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM faq_question_tags`).Scan(&n))
	require.Equal(t, 2, n, "duplicate normalized tag entries should collapse")

	var hasWB bool
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM faq_tags WHERE name = 'world-building')`).Scan(&hasWB))
	require.True(t, hasWB)
}

func TestEditQuestionForm(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Original title", "Original body", "x")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q/"+itoa(q.ID)+"/edit", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Original title")
	require.Contains(t, body, "Original body")
	require.Contains(t, body, `name="title"`)
	require.NotContains(t, body, "<!DOCTYPE") // partial only
}

func TestEditQuestionFormNonAuthorForbidden(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("eve")))
	q := seedQuestion(t, st, "alice", "Q", "body")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q/"+itoa(q.ID)+"/edit", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestEditQuestionFormAnonRejected(t *testing.T) {
	srv, st := freshServer(t)
	q := seedQuestion(t, st, "alice", "Q", "body")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/q/"+itoa(q.ID)+"/edit", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestEditQuestionSubmitHappyPath(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Original", "Original body", "x")

	form := url.Values{
		"title": {"Edited title"},
		"body":  {"Edited **body**"},
		"tags":  {"x, y"},
	}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, patchForm("/q/"+itoa(q.ID), form))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	body := rec.Body.String()
	require.Contains(t, body, "Edited title")
	require.Contains(t, body, "<strong>body</strong>") // GFM rendering ran

	// DB confirms locally_modified + edit_count++.
	var lm bool
	var ec int
	require.NoError(t, testPool.QueryRow(context.Background(),
		`SELECT locally_modified, edit_count FROM faq_questions WHERE id = $1`, q.ID).Scan(&lm, &ec))
	require.True(t, lm, "EditQuestion must set locally_modified=true")
	require.Equal(t, 1, ec)
}

func TestEditQuestionSubmitAdminAfterGrace(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	q := seedQuestion(t, st, "alice", "Original", "Original body")
	// Backdate created_at to outside the grace window.
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_questions SET created_at = now() - interval '1 hour' WHERE id = $1`, q.ID)
	require.NoError(t, err)

	form := url.Values{"title": {"Admin-edited"}, "body": {"new"}}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, patchForm("/q/"+itoa(q.ID), form))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Admin-edited")
}

func TestEditQuestionSubmitGraceExpired(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Original", "body")
	_, err := testPool.Exec(context.Background(),
		`UPDATE faq_questions SET created_at = now() - interval '1 hour' WHERE id = $1`, q.ID)
	require.NoError(t, err)

	form := url.Values{"title": {"too late"}, "body": {"x"}}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, patchForm("/q/"+itoa(q.ID), form))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestEditQuestionSubmitValidationKeepsForm(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	q := seedQuestion(t, st, "alice", "Q", "body")
	form := url.Values{"title": {""}, "body": {"x"}}
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, patchForm("/q/"+itoa(q.ID), form))
	// Validation failure on edit re-renders the edit form (200) with
	// errors — not a 422 like ask, because the htmx target stays the
	// form element either way.
	body := rec.Body.String()
	require.Contains(t, body, "title is required")
	require.Contains(t, body, "faq-form-edit")
}
