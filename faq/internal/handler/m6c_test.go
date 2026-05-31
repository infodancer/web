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

// TestM6c_Visibility_NativeUntaggedManualOnly — a native question with
// no subsite_id of its own gains visibility solely through an explicit
// faq_subsite_questions row.
func TestM6c_Visibility_NativeUntaggedManualOnly(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title: "Manually pinned", BodyMd: "b", BodyHtml: "<p>b</p>",
		AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)

	// Not visible yet — neither native owner nor tag rule nor pin.
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/x/q/"+strconv.FormatInt(q.ID, 10), nil))
	require.Equal(t, http.StatusNotFound, rec.Code,
		"untouched question must 404 on the subsite URL before pinning")

	require.NoError(t, st.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: sub.ID, QuestionID: q.ID,
	}))

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x/q", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Manually pinned")

	rec = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/x/q/"+strconv.FormatInt(q.ID, 10), nil))
	require.Equal(t, http.StatusOK, rec.Code,
		"pinned question must resolve on the subsite URL")
}

// TestM6c_Visibility_ImportTaggedAndPinnedNoDuplicate — pinning an
// import that already tag-matches the subsite must not double-list it.
// The visibility queries use UNION (not UNION ALL) for exactly this
// case.
func TestM6c_Visibility_ImportTaggedAndPinnedNoDuplicate(t *testing.T) {
	_, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "wot", DisplayName: "WoT"})
	require.NoError(t, err)
	tag, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "wheel-of-time"})
	require.NoError(t, err)
	require.NoError(t, st.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: sub.ID, TagID: tag.ID,
	}))
	src := mustHandlerSource(t, st, "forum.example.com")
	q := mustHandlerImportedQuestion(t, st, src, "Tag-matched and pinned")
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tag.ID,
	}))
	require.NoError(t, st.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: sub.ID, QuestionID: q.ID,
	}))

	count, err := st.CountSubsiteQuestions(ctx, &sub.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "tag-matched + pinned must dedupe to one row")
}

// TestM6c_Canonical_PinUniqueMatch — an untagged import pinned into
// exactly one subsite canonicalizes to that subsite (D8.3 + M6c).
func TestM6c_Canonical_PinUniqueMatch(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)
	src := mustHandlerSource(t, st, "forum.example.com")
	q := mustHandlerImportedQuestion(t, st, src, "Untagged import")
	require.NoError(t, st.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: sub.ID, QuestionID: q.ID,
	}))
	require.NoError(t, st.RecomputeCanonicalSubsites(ctx))

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/q/"+strconv.FormatInt(q.ID, 10), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(),
		`<link rel="canonical" href="/faq/x/q/`+strconv.FormatInt(q.ID, 10),
		"unique-pin import must canonicalize to the pinned subsite")
}

// TestM6c_Canonical_PinAmbiguous — an import that matches subsite A via
// a tag rule AND is explicitly pinned into subsite B must fall back to
// the global canonical (ambiguous between two subsites).
func TestM6c_Canonical_PinAmbiguous(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	subA, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "a", DisplayName: "A"})
	require.NoError(t, err)
	subB, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "b", DisplayName: "B"})
	require.NoError(t, err)
	tag, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "shared"})
	require.NoError(t, err)
	require.NoError(t, st.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: subA.ID, TagID: tag.ID,
	}))
	src := mustHandlerSource(t, st, "forum.example.com")
	q := mustHandlerImportedQuestion(t, st, src, "Tag A and pinned B")
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tag.ID,
	}))
	require.NoError(t, st.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: subB.ID, QuestionID: q.ID,
	}))
	require.NoError(t, st.RecomputeCanonicalSubsites(ctx))

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/q/"+strconv.FormatInt(q.ID, 10), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(),
		`<link rel="canonical" href="/faq/q/`+strconv.FormatInt(q.ID, 10),
		"tag-A + pin-B ambiguity must canonicalize to the global URL")
}

// TestM6c_Canonical_PinTagOverlapStillUnique — pin and tag rule both
// point at the *same* subsite. That's still one distinct subsite, so
// canonical = that subsite.
func TestM6c_Canonical_PinTagOverlapStillUnique(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)
	tag, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "t"})
	require.NoError(t, err)
	require.NoError(t, st.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: sub.ID, TagID: tag.ID,
	}))
	src := mustHandlerSource(t, st, "forum.example.com")
	q := mustHandlerImportedQuestion(t, st, src, "Tag + pin same subsite")
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tag.ID,
	}))
	require.NoError(t, st.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: sub.ID, QuestionID: q.ID,
	}))
	require.NoError(t, st.RecomputeCanonicalSubsites(ctx))

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/q/"+strconv.FormatInt(q.ID, 10), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(),
		`<link rel="canonical" href="/faq/x/q/`+strconv.FormatInt(q.ID, 10),
		"tag and pin pointing at the same subsite is still a unique match")
}

// TestM6c_Canonical_NativeOwnershipWinsOverPin — a native question
// owned by subsite X canonicalizes to X even if explicitly pinned into
// Y. Native ownership (D8.1) is the strongest signal.
func TestM6c_Canonical_NativeOwnershipWinsOverPin(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	subX, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)
	subY, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "y", DisplayName: "Y"})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title: "Native in X", BodyMd: "b", BodyHtml: "<p>b</p>",
		AuthorUserID: &alice.ID, SubsiteID: &subX.ID,
	})
	require.NoError(t, err)
	require.NoError(t, st.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: subY.ID, QuestionID: q.ID,
	}))
	require.NoError(t, st.RecomputeCanonicalSubsites(ctx))

	// Both subsites surface the question — verify by hitting Y's URL.
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/y/q/"+strconv.FormatInt(q.ID, 10), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(),
		`<link rel="canonical" href="/faq/x/q/`+strconv.FormatInt(q.ID, 10),
		"native ownership must beat explicit pin for canonical resolution")
}

// TestM6c_SeeAlso_IncludesPinnedSubsites — the see-also block on a
// detail page lists every subsite the question belongs to, including
// pin-only membership.
func TestM6c_SeeAlso_IncludesPinnedSubsites(t *testing.T) {
	srv, st := freshServer(t)
	ctx := context.Background()

	subA, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "aphid", DisplayName: "Aphid"})
	require.NoError(t, err)
	subB, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "beaver", DisplayName: "Beaver"})
	require.NoError(t, err)
	tag, err := st.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: "shared"})
	require.NoError(t, err)
	require.NoError(t, st.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: subA.ID, TagID: tag.ID,
	}))
	src := mustHandlerSource(t, st, "forum.example.com")
	q := mustHandlerImportedQuestion(t, st, src, "Tag A and pin B")
	require.NoError(t, st.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tag.ID,
	}))
	require.NoError(t, st.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: subB.ID, QuestionID: q.ID,
	}))

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/aphid/q/"+strconv.FormatInt(q.ID, 10), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Beaver", "pinned-only subsite must appear in see-also")
}

// TestM6c_CascadeOnSubsiteDelete — deleting a subsite removes its
// faq_subsite_questions rows via FK CASCADE.
func TestM6c_CascadeOnSubsiteDelete(t *testing.T) {
	_, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title: "T", BodyMd: "b", BodyHtml: "<p>b</p>", AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)
	require.NoError(t, st.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: sub.ID, QuestionID: q.ID,
	}))

	require.NoError(t, st.DeleteSubsite(ctx, sub.ID))

	var n int
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT COUNT(*)::INT FROM faq_subsite_questions WHERE subsite_id = $1`, sub.ID).Scan(&n))
	require.Equal(t, 0, n, "subsite delete must cascade to faq_subsite_questions")
}

// TestM6c_CascadeOnQuestionDelete — deleting a question (real DELETE,
// not the soft-delete status flip) removes its faq_subsite_questions
// rows.
func TestM6c_CascadeOnQuestionDelete(t *testing.T) {
	_, st := freshServer(t)
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title: "T", BodyMd: "b", BodyHtml: "<p>b</p>", AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)
	require.NoError(t, st.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: sub.ID, QuestionID: q.ID,
	}))

	_, err = testPool.Exec(ctx, `DELETE FROM faq_questions WHERE id = $1`, q.ID)
	require.NoError(t, err)

	var n int
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT COUNT(*)::INT FROM faq_subsite_questions WHERE question_id = $1`, q.ID).Scan(&n))
	require.Equal(t, 0, n, "question delete must cascade to faq_subsite_questions")
}

// TestM6c_AdminPinQuestion_HappyPath — admin POSTs to
// /admin/q/{id}/subsites and the question is pinned + canonical
// refreshed.
func TestM6c_AdminPinQuestion_HappyPath(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)
	src := mustHandlerSource(t, st, "forum.example.com")
	q := mustHandlerImportedQuestion(t, st, src, "Untagged import to pin")

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm(
		"/admin/q/"+strconv.FormatInt(q.ID, 10)+"/subsites",
		url.Values{"subsite_id": {strconv.FormatInt(sub.ID, 10)}},
	))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	in, err := st.IsQuestionExplicitlyInSubsite(ctx, store.IsQuestionExplicitlyInSubsiteParams{
		SubsiteID: sub.ID, QuestionID: q.ID,
	})
	require.NoError(t, err)
	require.True(t, in)

	// Canonical recompute ran as part of the handler, so the question
	// now canonicalizes to /x/.
	slug, err := st.GetCanonicalSubsiteSlugForQuestion(ctx, q.ID)
	require.NoError(t, err)
	require.Equal(t, "x", slug)
}

// TestM6c_AdminPinQuestion_RegularUserForbidden — only admins may pin.
func TestM6c_AdminPinQuestion_RegularUserForbidden(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	sub, err := st.CreateSubsite(context.Background(), store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm("/admin/q/1/subsites",
		url.Values{"subsite_id": {strconv.FormatInt(sub.ID, 10)}}))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestM6c_AdminUnpinQuestion_HappyPath — admin POSTs to
// /admin/q/{id}/subsites/{subsite_id}/delete and the pin is removed.
func TestM6c_AdminUnpinQuestion_HappyPath(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title: "T", BodyMd: "b", BodyHtml: "<p>b</p>", AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)
	require.NoError(t, st.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: sub.ID, QuestionID: q.ID,
	}))

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm(
		"/admin/q/"+strconv.FormatInt(q.ID, 10)+"/subsites/"+strconv.FormatInt(sub.ID, 10)+"/delete",
		nil,
	))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	in, err := st.IsQuestionExplicitlyInSubsite(ctx, store.IsQuestionExplicitlyInSubsiteParams{
		SubsiteID: sub.ID, QuestionID: q.ID,
	})
	require.NoError(t, err)
	require.False(t, in)
}

// TestM6c_AdminBulkPin_HappyPath — admin POSTs a whitespace-and-comma-
// separated list of IDs; valid ones pin and canonical is refreshed.
func TestM6c_AdminBulkPin_HappyPath(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")
	q1, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title: "One", BodyMd: "b", BodyHtml: "<p>b</p>", AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)
	q2, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title: "Two", BodyMd: "b", BodyHtml: "<p>b</p>", AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm(
		"/admin/subsites/"+strconv.FormatInt(sub.ID, 10)+"/questions",
		url.Values{"ids": {strconv.FormatInt(q1.ID, 10) + "  ,  " + strconv.FormatInt(q2.ID, 10)}},
	))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	count, err := st.CountExplicitSubsiteQuestions(ctx, sub.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
}

// TestM6c_AdminBulkPin_BadAndMissingTokens — non-numeric tokens and IDs
// that don't resolve to a question both surface back to the admin via
// the form; valid IDs still pin.
func TestM6c_AdminBulkPin_BadAndMissingTokens(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")
	q1, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title: "Valid", BodyMd: "b", BodyHtml: "<p>b</p>", AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm(
		"/admin/subsites/"+strconv.FormatInt(sub.ID, 10)+"/questions",
		url.Values{"ids": {strconv.FormatInt(q1.ID, 10) + " notanumber 99999"}},
	))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "notanumber", "bad token must be surfaced")
	require.Contains(t, body, "99999", "missing-ID must be surfaced")

	// Valid ID pinned anyway.
	count, err := st.CountExplicitSubsiteQuestions(ctx, sub.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

// TestM6c_AdminBulkPin_RegularUserForbidden — only admins may bulk-pin.
func TestM6c_AdminBulkPin_RegularUserForbidden(t *testing.T) {
	srv, st := freshServer(t, withResolver(regularUser("alice")))
	sub, err := st.CreateSubsite(context.Background(), store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm(
		"/admin/subsites/"+strconv.FormatInt(sub.ID, 10)+"/questions",
		url.Values{"ids": {"1 2 3"}},
	))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestM6c_AdminSubsiteRemoveQuestion — admin removes a pin from the
// subsite admin page.
func TestM6c_AdminSubsiteRemoveQuestion(t *testing.T) {
	srv, st := freshServer(t, withResolver(adminUser("root")))
	ctx := context.Background()

	sub, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "x", DisplayName: "X"})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title: "T", BodyMd: "b", BodyHtml: "<p>b</p>", AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)
	require.NoError(t, st.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: sub.ID, QuestionID: q.ID,
	}))

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, postForm(
		"/admin/subsites/"+strconv.FormatInt(sub.ID, 10)+"/questions/"+strconv.FormatInt(q.ID, 10)+"/delete",
		nil,
	))
	require.Equal(t, http.StatusSeeOther, rec.Code)

	count, err := st.CountExplicitSubsiteQuestions(ctx, sub.ID)
	require.NoError(t, err)
	require.EqualValues(t, 0, count)
}

// TestM6c_DetailPage_AdminCurateBlock — when an admin views a detail
// page, the curation block lists pinned subsites and available
// subsites to pin into. Anonymous viewers never see the block.
func TestM6c_DetailPage_AdminCurateBlock(t *testing.T) {
	ctx := context.Background()

	// Anonymous view — block absent.
	srvAnon, st := freshServer(t)
	subA, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "a", DisplayName: "Aphid"})
	require.NoError(t, err)
	_, err = st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "b", DisplayName: "Beaver"})
	require.NoError(t, err)
	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title: "T", BodyMd: "b", BodyHtml: "<p>b</p>", AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)
	require.NoError(t, st.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: subA.ID, QuestionID: q.ID,
	}))

	rec := httptest.NewRecorder()
	srvAnon.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/q/"+strconv.FormatInt(q.ID, 10), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "Pinned into",
		"anonymous viewers must not see the curation block")

	// Admin view — block present, both subsites listed (one pinned, one available).
	srvAdmin, _ := freshServer(t, withResolver(adminUser("root")))
	subA2, err := st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "a", DisplayName: "Aphid"})
	require.NoError(t, err)
	_, err = st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "b", DisplayName: "Beaver"})
	require.NoError(t, err)
	bob := mustHandlerUser(t, st, "bob")
	q2, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title: "T2", BodyMd: "b", BodyHtml: "<p>b</p>", AuthorUserID: &bob.ID,
	})
	require.NoError(t, err)
	require.NoError(t, st.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: subA2.ID, QuestionID: q2.ID,
	}))

	rec = httptest.NewRecorder()
	srvAdmin.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/q/"+strconv.FormatInt(q2.ID, 10), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Pinned into")
	require.Contains(t, body, "Aphid")  // pinned
	require.Contains(t, body, "Beaver") // available to pin
}

// An admin viewing an unpinned question sees no "Pinned into" heading or empty
// placeholder — just the Pin form when subsites exist, and no block at all when
// none do.
func TestM6c_DetailPage_NotPinnedHidesHeading(t *testing.T) {
	ctx := context.Background()
	srv, st := freshServer(t, withResolver(adminUser("root")))
	alice := mustHandlerUser(t, st, "alice")
	q, err := st.CreateQuestion(ctx, store.CreateQuestionParams{
		Title: "T", BodyMd: "b", BodyHtml: "<p>b</p>", AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)

	get := func() string {
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/q/"+strconv.FormatInt(q.ID, 10), nil))
		require.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}

	// No subsites at all → no curation block.
	body := get()
	require.NotContains(t, body, "Pinned into")
	require.NotContains(t, body, "Not pinned to any subsite")
	require.NotContains(t, body, "faq-admin-curate")

	// A subsite exists but the question is not pinned → Pin form only, no heading.
	_, err = st.CreateSubsite(ctx, store.CreateSubsiteParams{Slug: "x", DisplayName: "Xeno"})
	require.NoError(t, err)
	body = get()
	require.NotContains(t, body, "Pinned into", "no heading when the question isn't pinned")
	require.Contains(t, body, "Pin into subsite")
	require.Contains(t, body, "Xeno")
}
