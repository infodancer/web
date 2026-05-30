//go:build integration

package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

// TestSitemap_IndexIncludesSubsiteSections — the index references one
// /sitemap/subsite-{slug}-1.xml entry per subsite alongside the global
// /sitemap/1.xml entry.
func TestSitemap_IndexIncludesSubsiteSections(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()
	_, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)
	_, err = st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "brandon-sanderson", DisplayName: "BS",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	req.Host = "example.com"
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "https://example.com/faq/sitemap/1.xml")
	require.Contains(t, body, "https://example.com/faq/sitemap/subsite-wot-1.xml")
	require.Contains(t, body, "https://example.com/faq/sitemap/subsite-brandon-sanderson-1.xml")
}

// TestSitemap_SubsitePartEmitsPrefixedURLs — the per-subsite sub-
// sitemap emits URLs with the subsite prefix.
func TestSitemap_SubsitePartEmitsPrefixedURLs(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()
	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title: "Who is Rand?", BodyMd: "b", BodyHtml: "<p>b</p>",
		AuthorUserID: &alice.ID, SubsiteID: &sub.ID,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap/subsite-wot-1.xml", nil)
	req.Host = "example.com"
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	expected := "https://example.com/faq/wot/q/" + strconv.FormatInt(q.ID, 10) + "/who-is-rand"
	require.Contains(t, body, expected,
		"subsite sub-sitemap must emit /{subsite}/q/{id}/{slug}, got:\n%s", body)
}

// TestSitemap_SubsiteWithHyphenatedSlug — the slug "brandon-sanderson"
// has its own hyphen. The dispatcher must cut on the LAST hyphen
// before the partition number.
func TestSitemap_SubsiteWithHyphenatedSlug(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()
	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "brandon-sanderson", DisplayName: "BS",
	})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")
	_, err = st.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title: "Stormlight?", BodyMd: "b", BodyHtml: "<p>b</p>",
		AuthorUserID: &alice.ID, SubsiteID: &sub.ID,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap/subsite-brandon-sanderson-1.xml", nil)
	req.Host = "example.com"
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "https://example.com/faq/brandon-sanderson/q/")
}

// TestSitemap_UnknownSubsite404s — /sitemap/subsite-{unknown}-1.xml
// 404s rather than emitting an empty <urlset>.
func TestSitemap_UnknownSubsite404s(t *testing.T) {
	srv, _ := freshServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap/subsite-does-not-exist-1.xml", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestSitemap_GlobalPartStillWorks — the existing /sitemap/{N}.xml
// route is unchanged by the subsite extension.
func TestSitemap_GlobalPartStillWorks(t *testing.T) {
	srv, st := freshServer(t)
	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateQuestion(context.Background(), store.CreateQuestionParams{
		Title: "Global question", BodyMd: "b", BodyHtml: "<p>b</p>",
		AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap/1.xml", nil)
	req.Host = "example.com"
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.True(t, strings.Contains(body, "https://example.com/faq/q/"+strconv.FormatInt(q.ID, 10)),
		"global sub-sitemap must emit /q/{id}/{slug} (no subsite prefix)")
}
