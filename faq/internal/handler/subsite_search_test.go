package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

// TestSubsiteSearch_OnlyMatchesInSubsite — the same query word
// matched both subsite and out-of-subsite questions globally, but
// only the subsite-scoped ones appear in /{subsite}/search results.
func TestSubsiteSearch_OnlyMatchesInSubsite(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")

	// One native question in the subsite, containing the word "balefire".
	inSubsite, err := st.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title:        "Why is balefire dangerous?",
		BodyMd:       "Balefire unweaves the pattern.",
		BodyHtml:     "<p>Balefire unweaves the pattern.</p>",
		AuthorUserID: &alice.ID,
		SubsiteID:    &sub.ID,
	})
	require.NoError(t, err)

	// One native question NOT in the subsite, also containing the word.
	outOfSubsite, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title:        "Random balefire trivia",
		BodyMd:       "Balefire is also a song.",
		BodyHtml:     "<p>Balefire is also a song.</p>",
		AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wot/search?q="+url.QueryEscape("balefire"), nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, inSubsite.Title)
	require.NotContains(t, body, outOfSubsite.Title,
		"subsite search must not surface out-of-subsite hits")
}

// TestSubsiteSearch_EmptyQueryRendersForm — visiting /search without
// q= returns the form / empty state without hitting the DB.
func TestSubsiteSearch_EmptyQueryRendersForm(t *testing.T) {
	srv, st := freshServer(t)
	_, err := st.CreateSubsite(context.Background(), store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wot/search", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}
