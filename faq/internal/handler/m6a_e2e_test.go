//go:build integration

package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

// TestM6aEndToEnd_FullFlow exercises the M6a surface as a single
// admin+reader journey. The per-milestone tests check pieces in
// isolation; this one checks they compose correctly.
//
// Flow:
//  1. Admin creates a Brandon Sanderson subsite via the admin UI.
//  2. Admin attaches a "stormlight" tag rule via the admin UI.
//  3. A second-source import is seeded with one tagged question (direct
//     store; the importer is out of scope for M6a).
//  4. A native question is created in /bs via the store (the ask
//     handler is covered in M6a-8).
//  5. Directory at / shows the subsite card with both questions in
//     the count.
//  6. Subsite landing /bs shows the recent strip with both questions
//     and the stormlight tag chip.
//  7. Subsite list /bs/q surfaces both.
//  8. Short tag-filter /bs/stormlight shows only the tagged imported
//     question.
//  9. After RecomputeCanonicalSubsites, an imported question that
//     matches exactly one subsite via tag rules canonicalizes to that
//     subsite URL — not the global URL (D8.3 revised).
//  10. Native subsite-detail canonicalizes to the subsite URL.
//  11. Sitemap-index includes a subsite-bs-N.xml entry.
//  12. Admin re-scopes the native question to /wot; it disappears from
//     /bs and shows up in /wot.
func TestM6aEndToEnd_FullFlow(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	ctx := context.Background()

	// --- 1. Admin creates the subsites.
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/admin/subsites", url.Values{
		"slug":         {"bs"},
		"display_name": {"Brandon Sanderson"},
		"description":  {"FAQ for Brandon Sanderson's works."},
	}))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/admin/subsites", url.Values{
		"slug": {"wot"}, "display_name": {"Wheel of Time"},
	}))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	bs, err := st.GetSubsiteBySlug(ctx, "bs")
	require.NoError(t, err)
	wot, err := st.GetSubsiteBySlug(ctx, "wot")
	require.NoError(t, err)

	// --- 2. Admin attaches the stormlight tag rule.
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm(
		"/admin/subsites/"+strconv.FormatInt(bs.ID, 10)+"/tags",
		url.Values{"tag": {"stormlight"}},
	))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	// --- 3. Seed an imported question via the store (importer
	// behavior is M2's domain; here we just need an imported-shaped
	// row tagged with stormlight).
	src := mustHandlerSource(t, st, "forum.example.com")
	importedQ := mustHandlerImportedQuestion(t, st, src, "How does Stormlight work?")
	tag, err := st.GetTagByName(ctx, "stormlight")
	require.NoError(t, err)
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: importedQ.ID, TagID: tag.ID,
	}))

	// --- 4. Create the native question via the store (the ask
	// handler is covered in M6a-8; this avoids needing a second
	// server with a different resolver).
	alice := mustHandlerUser(t, st, "alice")
	nativeQ, err := st.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title:        "Who is Shallan?",
		BodyMd:       "Asking out of curiosity.",
		BodyHtml:     "<p>Asking out of curiosity.</p>",
		AuthorUserID: &alice.ID,
		SubsiteID:    &bs.ID,
	})
	require.NoError(t, err)

	// --- 5. Directory shows the subsite card with the right count.
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Brandon Sanderson")
	require.Contains(t, body, "2 questions")

	// --- 6. Subsite landing shows recent + tag chips.
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/bs", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body = rec.Body.String()
	require.Contains(t, body, "How does Stormlight work?")
	require.Contains(t, body, "Who is Shallan?")
	require.Contains(t, body, `href="/faq/bs/stormlight"`)

	// --- 7. Subsite list /bs/q surfaces both.
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/bs/q", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body = rec.Body.String()
	require.Contains(t, body, "How does Stormlight work?")
	require.Contains(t, body, "Who is Shallan?")

	// --- 8. Short tag-filter /bs/stormlight: only the tagged one.
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/bs/stormlight", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body = rec.Body.String()
	require.Contains(t, body, "How does Stormlight work?")
	require.NotContains(t, body, "Who is Shallan?",
		"tag filter must exclude native questions without the tag")

	// --- 9. After recompute, the imported question matches exactly
	// one subsite (bs/stormlight) and so canonicalizes there. Before
	// the recompute, canonical_subsite_id is NULL and the global URL
	// would win; running RecomputeCanonicalSubsites is part of the
	// host's startup contract.
	require.NoError(t, st.RecomputeCanonicalSubsites(ctx))
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/bs/q/"+strconv.FormatInt(importedQ.ID, 10), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body = rec.Body.String()
	require.Contains(t, body, `<link rel="canonical" href="/faq/bs/q/`+strconv.FormatInt(importedQ.ID, 10),
		"unique-match imported question must canonicalize to the subsite URL after recompute")

	// --- 10. Native detail canonicalizes to subsite.
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/bs/q/"+strconv.FormatInt(nativeQ.ID, 10), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body = rec.Body.String()
	require.Contains(t, body, `<link rel="canonical" href="/faq/bs/q/`+strconv.FormatInt(nativeQ.ID, 10),
		"native question must canonicalize to the subsite URL")

	// --- 11. Sitemap-index includes the subsite section.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	req.Host = "example.com"
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "subsite-bs-1.xml")

	// --- 12. Admin re-scopes the native question to /wot. It must
	// disappear from /bs/q and surface in /wot/q.
	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm(
		"/admin/q/"+strconv.FormatInt(nativeQ.ID, 10)+"/move",
		url.Values{"subsite_id": {strconv.FormatInt(wot.ID, 10)}},
	))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/bs/q", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "Who is Shallan?",
		"re-scoped native question must not surface in the old subsite")

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wot/q", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Who is Shallan?")
}
