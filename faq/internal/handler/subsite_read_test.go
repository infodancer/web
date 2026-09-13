package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

// TestSubsiteLanding_RendersDescriptionAndRecent — happy path for the
// /{subsite} landing: description + recent strip + tag chips, with
// breadcrumb back to the directory.
func TestSubsiteLanding_RendersDescriptionAndRecent(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug:        "wot",
		DisplayName: "Wheel of Time",
		Description: "FAQ for the WoT series.",
	})
	require.NoError(t, err)

	alice := mustHandlerUser(t, st, "alice")
	// Native question in the subsite.
	q, err := st.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title:        "Who is the Dragon Reborn?",
		BodyMd:       "Body.",
		BodyHtml:     "<p>Body.</p>",
		AuthorUserID: &alice.ID,
		SubsiteID:    &sub.ID,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wot", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Wheel of Time")
	require.Contains(t, body, "FAQ for the WoT series.")
	require.Contains(t, body, q.Title)
	require.Contains(t, body, `href="/faq/wot/q/`+strconv.FormatInt(q.ID, 10))
	require.Contains(t, body, `href="/faq/"`) // breadcrumb back to directory
}

// TestSubsiteLanding_EmptySubsite — landing renders even when the
// subsite has no questions and no description.
func TestSubsiteLanding_EmptySubsite(t *testing.T) {
	srv, st := freshServer(t)
	_, err := st.CreateSubsite(context.Background(), store.CreateSubsiteParams{
		Slug: "empty", DisplayName: "Empty",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/empty", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "No questions yet")
}

// TestSubsiteList_NativeAndImported — /{subsite}/q surfaces both
// native and tag-matched imported rows; the cards link to subsite-
// prefixed detail URLs.
func TestSubsiteList_NativeAndImported(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "bs", DisplayName: "Brandon Sanderson",
	})
	require.NoError(t, err)
	src := mustHandlerSource(t, st, "forum.example.com")
	tag, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "stormlight"})
	require.NoError(t, err)
	require.NoError(t, st.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: sub.ID, TagID: tag.ID,
	}))

	alice := mustHandlerUser(t, st, "alice")
	native, err := st.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title: "Native Q", BodyMd: "b", BodyHtml: "<p>b</p>",
		AuthorUserID: &alice.ID, SubsiteID: &sub.ID,
	})
	require.NoError(t, err)

	imported := mustHandlerImportedQuestion(t, st, src, "Imported Q")
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: imported.ID, TagID: tag.ID,
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/bs/q", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Native Q")
	require.Contains(t, body, "Imported Q")
	require.Contains(t, body, `href="/faq/bs/q/`+strconv.FormatInt(native.ID, 10))
	require.Contains(t, body, `href="/faq/bs/q/`+strconv.FormatInt(imported.ID, 10))
	require.Contains(t, body, "Archived") // locked import badge
}

// TestSubsiteDetail_NativeQuestion — happy path for /{subsite}/q/{id}.
// Canonical URL is the subsite URL for natives.
func TestSubsiteDetail_NativeQuestion(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "Wheel of Time",
	})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title:        "What is the One Power?",
		BodyMd:       "Asking for a friend.",
		BodyHtml:     "<p>Asking for a friend.</p>",
		AuthorUserID: &alice.ID,
		SubsiteID:    &sub.ID,
	})
	require.NoError(t, err)

	url := "/wot/q/" + strconv.FormatInt(q.ID, 10) + "/what-is-the-one-power"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "What is the One Power?")
	require.Contains(t, body, "Asking for a friend.")
	// Canonical = subsite URL for natives.
	require.Contains(t, body, `<link rel="canonical" href="/faq/wot/q/`+strconv.FormatInt(q.ID, 10))
}

// TestSubsiteDetail_ImportedUniqueMatchCanonicalToSubsite — an imported
// question whose tag rules match exactly one subsite canonicalizes to
// that subsite URL (D8.3 revised), once RecomputeCanonicalSubsites has
// run.
func TestSubsiteDetail_ImportedUniqueMatchCanonicalToSubsite(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)
	src := mustHandlerSource(t, st, "forum.example.com")
	tag, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "wheel-of-time"})
	require.NoError(t, err)
	require.NoError(t, st.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: sub.ID, TagID: tag.ID,
	}))
	q := mustHandlerImportedQuestion(t, st, src, "Imported about WoT")
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tag.ID,
	}))
	require.NoError(t, st.RecomputeCanonicalSubsites(ctx))

	url := "/wot/q/" + strconv.FormatInt(q.ID, 10) + "/imported-about-wot"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `<link rel="canonical" href="/faq/wot/q/`+strconv.FormatInt(q.ID, 10),
		"unique-subsite-match import must canonicalize to the subsite URL")
}

// TestSubsiteDetail_ImportedMultiMatchCanonicalToGlobal — an imported
// question whose tag rules match multiple subsites keeps the global URL
// as canonical (D8.3 revised: ambiguity falls back to global).
func TestSubsiteDetail_ImportedMultiMatchCanonicalToGlobal(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	wot, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)
	bs, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "bs", DisplayName: "BS",
	})
	require.NoError(t, err)

	src := mustHandlerSource(t, st, "forum.example.com")
	wotTag, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "wheel-of-time"})
	require.NoError(t, err)
	bsTag, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "brandon-sanderson"})
	require.NoError(t, err)
	require.NoError(t, st.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: wot.ID, TagID: wotTag.ID,
	}))
	require.NoError(t, st.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: bs.ID, TagID: bsTag.ID,
	}))

	q := mustHandlerImportedQuestion(t, st, src, "Cross-series question")
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: wotTag.ID,
	}))
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: bsTag.ID,
	}))
	require.NoError(t, st.RecomputeCanonicalSubsites(ctx))

	url := "/wot/q/" + strconv.FormatInt(q.ID, 10) + "/cross-series-question"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `<link rel="canonical" href="/faq/q/`+strconv.FormatInt(q.ID, 10),
		"multi-subsite-match import must canonicalize to the global URL")
}

// TestSubsiteDetail_404OnTagRuleMiss — a question that exists in the
// global pool but is not reachable from this subsite 404s on the
// subsite-prefixed URL.
func TestSubsiteDetail_404OnTagRuleMiss(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	// Subsite has no tag rules at all.
	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "isolated", DisplayName: "Isolated",
	})
	require.NoError(t, err)
	_ = sub

	// A native question in a different (non-existent) subsite.
	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title: "Unscoped", BodyMd: "b", BodyHtml: "<p>b</p>",
		AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)

	url := "/isolated/q/" + strconv.FormatInt(q.ID, 10)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestSubsiteDetail_SeeAlso — a question matching multiple subsites
// renders the see-also footer with links to the others.
func TestSubsiteDetail_SeeAlso(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	src := mustHandlerSource(t, st, "forum.example.com")
	wot, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "Wheel of Time",
	})
	require.NoError(t, err)
	bs, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "bs", DisplayName: "Brandon Sanderson",
	})
	require.NoError(t, err)

	tagWot, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "wheel-of-time"})
	require.NoError(t, err)
	tagBs, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "brandon-sanderson"})
	require.NoError(t, err)

	require.NoError(t, st.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: wot.ID, TagID: tagWot.ID,
	}))
	require.NoError(t, st.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: bs.ID, TagID: tagBs.ID,
	}))

	q := mustHandlerImportedQuestion(t, st, src, "Did Sanderson change Rand's arc?")
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tagWot.ID,
	}))
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tagBs.ID,
	}))

	url := "/wot/q/" + strconv.FormatInt(q.ID, 10)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Also in")
	require.Contains(t, body, `href="/faq/bs/q/`+strconv.FormatInt(q.ID, 10))
	require.NotContains(t, body, `href="/faq/wot/q/`+strconv.FormatInt(q.ID, 10)+`">Wheel of Time</a>`,
		"see-also must not list the current subsite")
}

// TestSubsiteDetail_CanonicalSlugRedirect — wrong slug in URL 301s to
// the subsite-prefixed canonical, not the global one.
func TestSubsiteDetail_CanonicalSlugRedirect(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title:  "What is the One Power?",
		BodyMd: "b", BodyHtml: "<p>b</p>",
		AuthorUserID: &alice.ID, SubsiteID: &sub.ID,
	})
	require.NoError(t, err)

	url := "/wot/q/" + strconv.FormatInt(q.ID, 10) + "/wrong-slug"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusMovedPermanently, rec.Code)
	loc := rec.Header().Get("Location")
	require.Contains(t, loc, "/faq/wot/q/"+strconv.FormatInt(q.ID, 10)+"/what-is-the-one-power",
		"redirect should preserve the subsite prefix, not bounce to /faq/q/...")
}

// --- helpers ---

func mustHandlerUser(t *testing.T, st *store.Store, name string) store.FaqUsers {
	t.Helper()
	hash := make([]byte, 32)
	copy(hash, name)
	u, err := st.UpsertUser(context.Background(), store.UpsertUserParams{
		SubHash: hash, DisplayName: name,
	})
	require.NoError(t, err)
	return u
}

func mustHandlerSource(t *testing.T, st *store.Store, domain string) store.FaqSources {
	t.Helper()
	src, err := st.UpsertSource(context.Background(), store.UpsertSourceParams{
		SourceDomain: domain, DisplayName: domain, License: "CC BY-SA 4.0",
		DumpDate:  pgtype.Date{Time: time.Date(2023, 9, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		PostCount: 0,
	})
	require.NoError(t, err)
	return src
}

var handlerImportedPostCounter int64 = 5000

func mustHandlerImportedQuestion(t *testing.T, st *store.Store, src store.FaqSources, title string) store.FaqQuestions {
	t.Helper()
	handlerImportedPostCounter++
	postID := handlerImportedPostCounter
	url := "https://example.test/q/" + title
	q, err := st.CreateImportedQuestion(context.Background(), store.CreateImportedQuestionParams{
		Title: title, BodyMd: "Imported body", BodyHtml: "<p>Imported body</p>",
		SourceID: &src.ID, SourcePostID: &postID, SourceUrl: &url,
		NeedsReview: false,
		CreatedAt:   pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	})
	require.NoError(t, err)
	return q
}
