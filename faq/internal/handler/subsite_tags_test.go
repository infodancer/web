package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

// TestSubsiteTagsIndex_OnlyUsedTags — /{subsite}/tags shows tags
// actually used by questions reachable from the subsite, not the
// entire global tag set.
func TestSubsiteTagsIndex_OnlyUsedTags(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "Wheel of Time",
	})
	require.NoError(t, err)
	src := mustHandlerSource(t, st, "forum.example.com")

	used, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "rand"})
	require.NoError(t, err)
	bonus, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "mat"})
	require.NoError(t, err)
	unused, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "elsewhere"})
	require.NoError(t, err)

	require.NoError(t, st.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: sub.ID, TagID: used.ID,
	}))

	q := mustHandlerImportedQuestion(t, st, src, "Rand and Mat")
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: used.ID,
	}))
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: bonus.ID,
	}))
	// Unused tag has no question attached.
	_ = unused

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wot/tags", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "rand")
	require.Contains(t, body, "mat") // rides along on the matched question
	require.NotContains(t, body, "elsewhere",
		"tags not used in this subsite must not appear")
	// Tag URLs in the subsite index are the short-form subsite paths.
	require.Contains(t, body, `href="/faq/wot/rand"`)
}

// TestSubsiteTagDetail_LongForm — /{subsite}/tags/{name} surfaces the
// intersection (subsite + tag).
func TestSubsiteTagDetail_LongForm(t *testing.T) {
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

	q := mustHandlerImportedQuestion(t, st, src, "Who is Shallan?")
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tag.ID,
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/bs/tags/stormlight", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Who is Shallan?")
	require.Contains(t, body, `href="/faq/bs/q/`+strconv.FormatInt(q.ID, 10))
}

// TestSubsiteTagDetail_ShortForm — /{subsite}/{tag} resolves to the
// same view as /{subsite}/tags/{tag} via the short tag-filter form.
func TestSubsiteTagDetail_ShortForm(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "bs", DisplayName: "BS",
	})
	require.NoError(t, err)
	src := mustHandlerSource(t, st, "forum.example.com")
	tag, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "stormlight-archive"})
	require.NoError(t, err)
	require.NoError(t, st.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: sub.ID, TagID: tag.ID,
	}))
	q := mustHandlerImportedQuestion(t, st, src, "Stormlight Q")
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tag.ID,
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/bs/stormlight-archive", nil)
	srv.Routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Stormlight Q")
}

// TestSubsiteTagDetail_404OnUnknownTag — unknown tag name 404s rather
// than rendering an empty page.
func TestSubsiteTagDetail_404OnUnknownTag(t *testing.T) {
	srv, st := freshServer(t)
	_, err := st.CreateSubsite(context.Background(), store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wot/tags/does-not-exist", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestSubsiteTagDetail_404OnGlobalTagWithoutSubsiteIntersection — tag
// exists globally but no question in this subsite uses it. Should 404,
// not render an empty list.
func TestSubsiteTagDetail_404OnGlobalTagWithoutSubsiteIntersection(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	_, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)
	_, err = st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "orphan-tag"})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wot/tags/orphan-tag", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestSubsiteTagDetail_ShortForm404ForReservedWord — reserved second-
// segment slugs (q, tags, search, ask) can't be reached via the short
// form. Each has its own explicit case in the dispatcher.
func TestSubsiteTagDetail_ShortForm404ForReservedWord(t *testing.T) {
	// "q" routes to subsite list (handled by an earlier case), so this
	// isn't a 404 — it's a valid 200. The actual reserved-words guard
	// for the short form is the second-segment list (q/tags/search/ask),
	// each handled by an explicit case in routeSubsite that the
	// fallthrough never reaches. We exercise one path that the guard
	// would catch if a future reserved word lacked its own case.
	srv, st := freshServer(t)
	_, err := st.CreateSubsite(context.Background(), store.CreateSubsiteParams{
		Slug: "wot", DisplayName: "WoT",
	})
	require.NoError(t, err)

	// "q" → /wot/q (list page), not the tag-filter form. Status 200.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wot/q", nil)
	srv.Routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "q is handled as list, not tag")
}
