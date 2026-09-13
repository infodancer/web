package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchEmptyQueryShowsPrompt(t *testing.T) {
	srv, _ := freshServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Enter a search term")
}

func TestSearchTitleMatch(t *testing.T) {
	srv, st := freshServer(t)
	seedQuestion(t, st, "alice", "counterspell mechanics", "irrelevant body")
	seedQuestion(t, st, "alice", "Hex spell", "some discussion of counterspell here")
	seedQuestion(t, st, "alice", "off-topic", "doesn't match")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/search?q=counterspell", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	titleIdx := strings.Index(body, "counterspell mechanics")
	bodyIdx := strings.Index(body, "Hex spell")
	require.GreaterOrEqual(t, titleIdx, 0, "title match missing")
	require.GreaterOrEqual(t, bodyIdx, 0, "body match missing")
	require.Less(t, titleIdx, bodyIdx, "title match should rank above body match")
	require.NotContains(t, body, "off-topic")
}

func TestSearchNoResults(t *testing.T) {
	srv, st := freshServer(t)
	seedQuestion(t, st, "alice", "Existing", "body")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/search?q=nonexistent", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "No questions match")
}

func TestSearchHTMXReturnsPartial(t *testing.T) {
	srv, st := freshServer(t)
	seedQuestion(t, st, "alice", "findme", "body")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/search?q=findme", nil)
	req.Header.Set("HX-Request", "true")
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.NotContains(t, body, "<!DOCTYPE")
	require.NotContains(t, body, "<form")
	require.Contains(t, body, "findme")
}

func TestSearchDeletedQuestionExcluded(t *testing.T) {
	srv, st := freshServer(t)
	q := seedQuestion(t, st, "alice", "findme", "body")
	_, err := testPool.Exec(context.Background(), `UPDATE faq_questions SET status='deleted' WHERE id=$1`, q.ID)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	hreq := httptest.NewRequest(http.MethodGet, "/search?q=findme", nil)
	srv.Routes().ServeHTTP(rec, hreq)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "No questions match")
}
