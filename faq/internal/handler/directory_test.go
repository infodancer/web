//go:build integration

package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

// TestDirectory_EmptyDB renders the directory landing on an empty
// corpus. No subsites, no recent questions — but the heading and the
// "no questions" fallback still render.
func TestDirectory_EmptyDB(t *testing.T) {
	srv, _ := freshServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "FAQ")
	require.Contains(t, body, "No questions yet")
}

// TestDirectory_RendersSubsiteCards — every existing subsite shows up
// as a card with its display name, description, and question count.
func TestDirectory_RendersSubsiteCards(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	_, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug:        "brandon-sanderson",
		DisplayName: "Brandon Sanderson",
		Description: "FAQ for Brandon Sanderson's published works.",
	})
	require.NoError(t, err)
	_, err = st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug:        "wheel-of-time",
		DisplayName: "Wheel of Time",
		Description: "Robert Jordan and Brandon Sanderson's series.",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Brandon Sanderson")
	require.Contains(t, body, "FAQ for Brandon Sanderson&#39;s published works.")
	require.Contains(t, body, "Wheel of Time")
	require.Contains(t, body, "Robert Jordan")
	require.Contains(t, body, `href="/faq/brandon-sanderson"`)
	require.Contains(t, body, `href="/faq/wheel-of-time"`)
}

// TestDirectory_RecentStripUsesBasePathLinks — the recent-questions
// strip emits absolute /faq/q/{id}/{slug} links, not the relative
// ../q/{id}/{slug} form used on depth-1 pages.
func TestDirectory_RecentStripUsesBasePathLinks(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	alice, err := st.UpsertUser(ctx, store.UpsertUserParams{
		SubHash:     []byte("0123456789012345678901234567890a"),
		DisplayName: "Alice",
	})
	require.NoError(t, err)

	q, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title:        "What is the One Power?",
		BodyMd:       "Asking for a friend.",
		BodyHtml:     "<p>Asking for a friend.</p>",
		AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "What is the One Power?")
	// Absolute URL with the mount path prefix:
	require.Contains(t, body, `href="/faq/q/`+formatID(q.ID))
}

func formatID(id int64) string {
	// Helper to avoid pulling in strconv just for one printf.
	s := ""
	if id == 0 {
		return "0"
	}
	for id > 0 {
		s = string(rune('0'+(id%10))) + s
		id /= 10
	}
	return s
}
