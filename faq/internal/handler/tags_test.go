//go:build integration

package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTagsIndex(t *testing.T) {
	srv, st := freshServer(t)
	seedQuestion(t, st, "alice", "Q1", "body", "dnd-5e", "spells")
	seedQuestion(t, st, "alice", "Q2", "body", "dnd-5e")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tags", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Tags")
	require.Contains(t, body, "dnd-5e")
	require.Contains(t, body, "spells")
	// dnd-5e appears twice (count=2) — make sure it appears before spells
	// (count=1) in the descending sort order.
	dnd := strings.Index(body, "dnd-5e")
	spells := strings.Index(body, "spells")
	require.GreaterOrEqual(t, dnd, 0)
	require.GreaterOrEqual(t, spells, 0)
	require.Less(t, dnd, spells, "dnd-5e (2 questions) should sort before spells (1)")
}

func TestTagsIndexEmpty(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tags", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "No tags yet.")
}

func TestTagDetailHappyPath(t *testing.T) {
	srv, st := freshServer(t)
	seedQuestion(t, st, "alice", "Targeted Q", "body", "dnd-5e")
	seedQuestion(t, st, "alice", "Off-tag Q", "body", "boardgames")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tags/dnd-5e", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Targeted Q")
	require.NotContains(t, body, "Off-tag Q")
	require.Contains(t, body, "Questions tagged")
}

func TestTagDetailUnknownTag404(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tags/no-such-tag", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTagDetailHTMXReturnsPartial(t *testing.T) {
	srv, st := freshServer(t)
	seedQuestion(t, st, "alice", "Q", "body", "x")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tags/x", nil)
	req.Header.Set("HX-Request", "true")
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.NotContains(t, body, "<!DOCTYPE")
	require.NotContains(t, body, "<title>")
	require.Contains(t, body, "Q")
}
