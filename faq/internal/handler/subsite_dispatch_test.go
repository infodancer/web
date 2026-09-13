package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

// TestSubsiteDispatch_LiteralRoutesStillWin asserts that registering the
// /{subsite} wildcard family did not steal traffic from the existing
// literal routes (/q, /tags, /search, /sitemap.xml, /admin/*).
func TestSubsiteDispatch_LiteralRoutesStillWin(t *testing.T) {
	srv, _ := freshServer(t)

	cases := []struct {
		path string
	}{
		{"/q"},
		{"/tags"},
		{"/search"},
		{"/sitemap.xml"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			srv.Routes().ServeHTTP(rec, req)
			// Each literal route is wired and returns 200 OK with its
			// own body. The exact body shape is covered by per-route
			// tests; here we only assert "not the subsite stub".
			require.NotEqual(t, "subsite=q name=q", rec.Body.String(),
				"literal /q must not be eaten by /{subsite}")
			require.NotEqual(t, http.StatusNotFound, rec.Code,
				"literal route %s should be wired", tc.path)
		})
	}
}

// TestSubsiteDispatch_UnknownSlug404s verifies the resolver 404s when
// the {subsite} segment doesn't match any row.
func TestSubsiteDispatch_UnknownSlug404s(t *testing.T) {
	srv, _ := freshServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestSubsiteDispatch_KnownSlugRoutes verifies that a real subsite slug
// reaches the subsite landing handler — body contains the subsite's
// display name and breadcrumbs.
func TestSubsiteDispatch_KnownSlugRoutes(t *testing.T) {
	srv, st := freshServer(t)
	mustCreateSubsite(t, st, "brandon-sanderson", "Brandon Sanderson")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/brandon-sanderson", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Brandon Sanderson",
		"landing must render subsite display name")
}

// TestSubsiteDispatch_NestedRoutes verifies the /{subsite}/q,
// /{subsite}/tags, /{subsite}/search, /{subsite}/ask, /{subsite}/{tag}
// patterns all route through subsiteDispatch with the right subsite
// resolved. Status code 200 (or 404 for paths that don't yet have
// content) is sufficient — handler behavior is covered in the
// per-route tests.
func TestSubsiteDispatch_NestedRoutes(t *testing.T) {
	srv, st := freshServer(t)
	mustCreateSubsite(t, st, "wot", "Wheel of Time")

	cases := []struct {
		method     string
		path       string
		wantStatus int
	}{
		{http.MethodGet, "/wot/q", http.StatusOK},                  // empty list still 200s
		{http.MethodGet, "/wot/q/123", http.StatusNotFound},        // question 123 doesn't exist
		{http.MethodGet, "/wot/q/123/slug", http.StatusNotFound},   // same
		{http.MethodGet, "/wot/tags", http.StatusOK},               // empty tag index renders
		{http.MethodGet, "/wot/tags/rand", http.StatusNotFound},    // tag doesn't exist
		{http.MethodGet, "/wot/search", http.StatusOK},             // search-stub
		{http.MethodGet, "/wot/ask", http.StatusUnauthorized},      // anon → 401
		{http.MethodGet, "/wot/any-tag-slug", http.StatusNotFound}, // tag doesn't exist
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			srv.Routes().ServeHTTP(rec, req)
			require.Equal(t, tc.wantStatus, rec.Code, "body: %s", rec.Body.String())
		})
	}
}

// TestSubsiteDispatch_NestedUnknownSlug404s verifies a nested route
// with an unknown {subsite} 404s.
func TestSubsiteDispatch_NestedUnknownSlug404s(t *testing.T) {
	srv, _ := freshServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/no-such-subsite/q", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestSubsiteDispatch_DirectoryRendersAtRoot — the / route reaches the
// directory landing handler (not the subsite resolver, because /
// matches the literal /{$} pattern on the mux).
func TestSubsiteDispatch_DirectoryRendersAtRoot(t *testing.T) {
	srv, _ := freshServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "FAQ", "directory page renders the heading")
}

// --- helpers ---

func mustCreateSubsite(t *testing.T, st *store.Store, slug, name string) store.FaqSubsites {
	t.Helper()
	sub, err := st.CreateSubsite(context.Background(), store.CreateSubsiteParams{
		Slug:        slug,
		DisplayName: name,
		Description: "",
	})
	require.NoError(t, err)
	return sub
}
