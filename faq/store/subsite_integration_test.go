package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

// TestCreateSubsite_HappyPath verifies basic insert/read round-trip.
func TestCreateSubsite_HappyPath(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	created, err := s.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug:        "brandon-sanderson",
		DisplayName: "Brandon Sanderson",
		Description: "FAQ for Brandon Sanderson works.",
	})
	require.NoError(t, err)
	require.Equal(t, "brandon-sanderson", created.Slug)

	got, err := s.GetSubsiteBySlug(ctx, "brandon-sanderson")
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "Brandon Sanderson", got.DisplayName)
}

// TestCreateSubsite_UniqueSlug enforces the slug uniqueness constraint.
func TestCreateSubsite_UniqueSlug(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	_, err := s.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug:        "wheel-of-time",
		DisplayName: "Wheel of Time",
	})
	require.NoError(t, err)

	_, err = s.CreateSubsite(ctx, store.CreateSubsiteParams{
		Slug:        "wheel-of-time",
		DisplayName: "WoT duplicate",
	})
	require.Error(t, err, "second insert with same slug must fail")
}

// TestSubsite_NativeQuestion_AppearsInListAndCount covers the simple native
// case: a question with subsite_id set is in the subsite's list and count.
func TestSubsite_NativeQuestion_AppearsInListAndCount(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	alice := mustUser(t, s, "alice")
	sub := mustSubsite(t, s, "honor-harrington", "Honor Harrington")

	q, err := s.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title:        "Is Honor a Mary Sue?",
		BodyMd:       "I keep hearing this claim, but...",
		BodyHtml:     "<p>I keep hearing this claim, but...</p>",
		AuthorUserID: &alice.ID,
		SubsiteID:    &sub.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, q.SubsiteID)
	require.Equal(t, sub.ID, *q.SubsiteID)

	rows, err := s.ListSubsiteQuestionsNewest(ctx, store.ListSubsiteQuestionsNewestParams{
		SubsiteID: &sub.ID,
		LimitN:    25,
		OffsetN:   0,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, q.ID, rows[0].ID)

	n, err := s.CountSubsiteQuestions(ctx, &sub.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

// TestSubsite_ImportedQuestion_MatchesViaTagRule covers the tag-rule path:
// an imported question is reached through faq_subsite_tags, never via
// subsite_id (which stays NULL on imports).
func TestSubsite_ImportedQuestion_MatchesViaTagRule(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	src := mustSource(t, s, "forum.example.com")
	sub := mustSubsite(t, s, "wheel-of-time", "Wheel of Time")
	tag := mustTag(t, s, "wheel-of-time")

	require.NoError(t, s.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: sub.ID,
		TagID:     tag.ID,
	}))

	q := mustImportedQuestion(t, s, src, "Who is the Dragon Reborn?")
	require.NoError(t, s.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID,
		TagID:      tag.ID,
	}))
	// Confirm imports keep subsite_id NULL — membership is computed.
	got, err := s.GetQuestion(ctx, q.ID)
	require.NoError(t, err)
	require.Nil(t, got.SubsiteID, "imported rows must not have subsite_id written")

	rows, err := s.ListSubsiteQuestionsNewest(ctx, store.ListSubsiteQuestionsNewestParams{
		SubsiteID: &sub.ID,
		LimitN:    25,
		OffsetN:   0,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, q.ID, rows[0].ID)

	n, err := s.CountSubsiteQuestions(ctx, &sub.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

// TestSubsite_NativeWithoutSubsite_NotInAnySubsite — pre-existing native
// questions (subsite_id NULL) appear only on the global list per D8.7.
func TestSubsite_NativeWithoutSubsite_NotInAnySubsite(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	alice := mustUser(t, s, "alice")
	sub := mustSubsite(t, s, "any", "Any")

	_ = mustQuestion(t, s, alice, "Ungrouped question")

	rows, err := s.ListSubsiteQuestionsNewest(ctx, store.ListSubsiteQuestionsNewestParams{
		SubsiteID: &sub.ID,
		LimitN:    25,
		OffsetN:   0,
	})
	require.NoError(t, err)
	require.Empty(t, rows, "subsite_id IS NULL natives must not surface in any subsite")
}

// TestSubsite_DeletedQuestion_Excluded — soft-deleted rows do not leak.
func TestSubsite_DeletedQuestion_Excluded(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	alice := mustUser(t, s, "alice")
	sub := mustSubsite(t, s, "wot", "WoT")
	q, err := s.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title: "Q", BodyMd: "b", BodyHtml: "<p>b</p>",
		AuthorUserID: &alice.ID, SubsiteID: &sub.ID,
	})
	require.NoError(t, err)

	require.NoError(t, s.SetQuestionStatus(ctx, store.SetQuestionStatusParams{
		ID: q.ID, Status: store.FaqQuestionStatusDeleted,
	}))

	rows, err := s.ListSubsiteQuestionsNewest(ctx, store.ListSubsiteQuestionsNewestParams{
		SubsiteID: &sub.ID, LimitN: 25, OffsetN: 0,
	})
	require.NoError(t, err)
	require.Empty(t, rows)
}

// TestSubsite_LockedImport_StillVisible — locked is not deleted; locked
// imports remain visible in their subsites (they are the bulk of the corpus).
func TestSubsite_LockedImport_StillVisible(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	src := mustSource(t, s, "forum.example.com")
	sub := mustSubsite(t, s, "scifi", "Scifi")
	tag := mustTag(t, s, "asimov")
	require.NoError(t, s.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: sub.ID, TagID: tag.ID,
	}))

	q := mustImportedQuestion(t, s, src, "Robots and Empire?")
	require.NoError(t, s.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tag.ID,
	}))
	// imports default to 'locked' (per CreateImportedQuestion behavior)
	require.Equal(t, store.FaqQuestionStatusLocked, q.Status)

	rows, err := s.ListSubsiteQuestionsNewest(ctx, store.ListSubsiteQuestionsNewestParams{
		SubsiteID: &sub.ID, LimitN: 25, OffsetN: 0,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

// TestSubsite_IsQuestionInSubsite_404Gate — the detail-page membership probe.
func TestSubsite_IsQuestionInSubsite_404Gate(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	alice := mustUser(t, s, "alice")
	subA := mustSubsite(t, s, "a", "A")
	subB := mustSubsite(t, s, "b", "B")

	qInA, err := s.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
		Title: "Q in A", BodyMd: "b", BodyHtml: "<p>b</p>",
		AuthorUserID: &alice.ID, SubsiteID: &subA.ID,
	})
	require.NoError(t, err)

	inA, err := s.IsQuestionInSubsite(ctx, store.IsQuestionInSubsiteParams{
		QuestionID: qInA.ID, SubsiteID: &subA.ID,
	})
	require.NoError(t, err)
	require.True(t, inA, "qInA must be in subsite A")

	inB, err := s.IsQuestionInSubsite(ctx, store.IsQuestionInSubsiteParams{
		QuestionID: qInA.ID, SubsiteID: &subB.ID,
	})
	require.NoError(t, err)
	require.False(t, inB, "qInA must NOT be in subsite B (different subsite)")
}

// TestSubsite_ListSubsitesForQuestion_CrossMatch — see-also block driver.
// One imported question tagged with rules in two subsites surfaces both.
func TestSubsite_ListSubsitesForQuestion_CrossMatch(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	src := mustSource(t, s, "forum.example.com")
	wot := mustSubsite(t, s, "wot", "WoT")
	bs := mustSubsite(t, s, "bs", "Brandon Sanderson")

	tagWot := mustTag(t, s, "wheel-of-time")
	tagBs := mustTag(t, s, "brandon-sanderson")

	require.NoError(t, s.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: wot.ID, TagID: tagWot.ID,
	}))
	require.NoError(t, s.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: bs.ID, TagID: tagBs.ID,
	}))

	q := mustImportedQuestion(t, s, src, "Did Sanderson change Rand's arc?")
	require.NoError(t, s.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tagWot.ID,
	}))
	require.NoError(t, s.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tagBs.ID,
	}))

	subsites, err := s.ListSubsitesForQuestion(ctx, q.ID)
	require.NoError(t, err)
	require.Len(t, subsites, 2)
	// Sorted by display_name: "Brandon Sanderson" before "WoT".
	require.Equal(t, "Brandon Sanderson", subsites[0].DisplayName)
	require.Equal(t, "WoT", subsites[1].DisplayName)
}

// TestSubsite_DetachTag_RemovesFromSubsite — dropping a tag rule excludes
// imports that were only reachable via that rule.
func TestSubsite_DetachTag_RemovesFromSubsite(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	src := mustSource(t, s, "forum.example.com")
	sub := mustSubsite(t, s, "wot", "WoT")
	tag := mustTag(t, s, "wheel-of-time")
	require.NoError(t, s.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: sub.ID, TagID: tag.ID,
	}))

	q := mustImportedQuestion(t, s, src, "Wheel question")
	require.NoError(t, s.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tag.ID,
	}))

	n, err := s.CountSubsiteQuestions(ctx, &sub.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	require.NoError(t, s.DetachTagFromSubsite(ctx, store.DetachTagFromSubsiteParams{
		SubsiteID: sub.ID, TagID: tag.ID,
	}))

	n, err = s.CountSubsiteQuestions(ctx, &sub.ID)
	require.NoError(t, err)
	require.Equal(t, int64(0), n, "detaching the only rule must empty the subsite")
}

// TestSubsite_DeleteSubsite_CascadesTagRules — DROP TABLE ON DELETE CASCADE.
func TestSubsite_DeleteSubsite_CascadesTagRules(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	sub := mustSubsite(t, s, "drop", "Drop")
	tag := mustTag(t, s, "tag")
	require.NoError(t, s.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: sub.ID, TagID: tag.ID,
	}))

	require.NoError(t, s.DeleteSubsite(ctx, sub.ID))

	// faq_subsite_tags row should be gone via ON DELETE CASCADE.
	rules, err := s.ListTagRulesForSubsite(ctx, sub.ID)
	require.NoError(t, err)
	require.Empty(t, rules)
}

// TestSubsite_ListSubsites_CountsBothBranches — admin list shows native +
// tag-matched imported, deduped.
func TestSubsite_ListSubsites_CountsBothBranches(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	alice := mustUser(t, s, "alice")
	src := mustSource(t, s, "forum.example.com")
	sub := mustSubsite(t, s, "mixed", "Mixed")
	tag := mustTag(t, s, "mixed-tag")
	require.NoError(t, s.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: sub.ID, TagID: tag.ID,
	}))

	// 2 native questions in the subsite.
	for i := 0; i < 2; i++ {
		_, err := s.CreateNativeQuestionInSubsite(ctx, store.CreateNativeQuestionInSubsiteParams{
			Title: "native", BodyMd: "b", BodyHtml: "<p>b</p>",
			AuthorUserID: &alice.ID, SubsiteID: &sub.ID,
		})
		require.NoError(t, err)
	}
	// 1 imported question tagged into the subsite.
	q := mustImportedQuestion(t, s, src, "import")
	require.NoError(t, s.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: tag.ID,
	}))

	rows, err := s.ListSubsites(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(3), rows[0].QuestionCount, "should count 2 native + 1 imported")
}

// TestSubsite_SetQuestionSubsite_OnlyNative — admin re-scope must not touch
// imported questions (the SQL guards source_id IS NULL).
func TestSubsite_SetQuestionSubsite_OnlyNative(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	alice := mustUser(t, s, "alice")
	src := mustSource(t, s, "forum.example.com")
	sub := mustSubsite(t, s, "target", "Target")

	nativeQ := mustQuestion(t, s, alice, "Native Q")
	importedQ := mustImportedQuestion(t, s, src, "Imported Q")

	require.NoError(t, s.SetQuestionSubsite(ctx, store.SetQuestionSubsiteParams{
		ID: nativeQ.ID, SubsiteID: &sub.ID,
	}))
	require.NoError(t, s.SetQuestionSubsite(ctx, store.SetQuestionSubsiteParams{
		ID: importedQ.ID, SubsiteID: &sub.ID,
	}))

	gotNative, err := s.GetQuestion(ctx, nativeQ.ID)
	require.NoError(t, err)
	require.NotNil(t, gotNative.SubsiteID)
	require.Equal(t, sub.ID, *gotNative.SubsiteID)

	gotImported, err := s.GetQuestion(ctx, importedQ.ID)
	require.NoError(t, err)
	require.Nil(t, gotImported.SubsiteID, "imported subsite_id must not be writable")
}

// TestSubsite_ListTopTagsForSubsite_OnlyUsedTags — landing page tag chips.
func TestSubsite_ListTopTagsForSubsite_OnlyUsedTags(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	src := mustSource(t, s, "forum.example.com")
	sub := mustSubsite(t, s, "x", "X")
	ruleTag := mustTag(t, s, "rule-tag")
	bonusTag := mustTag(t, s, "bonus-tag")
	unusedTag := mustTag(t, s, "unused-tag")

	require.NoError(t, s.AttachTagToSubsite(ctx, store.AttachTagToSubsiteParams{
		SubsiteID: sub.ID, TagID: ruleTag.ID,
	}))

	q := mustImportedQuestion(t, s, src, "Q")
	require.NoError(t, s.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: ruleTag.ID,
	}))
	require.NoError(t, s.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
		QuestionID: q.ID, TagID: bonusTag.ID,
	}))

	tags, err := s.ListTopTagsForSubsite(ctx, store.ListTopTagsForSubsiteParams{
		SubsiteID: &sub.ID, LimitN: 10,
	})
	require.NoError(t, err)
	// Both rule-tag (matches by rule) and bonus-tag (rides along on the
	// matched question) appear. unused-tag must not.
	names := make(map[string]bool)
	for _, tag := range tags {
		names[tag.Name] = true
	}
	require.True(t, names["rule-tag"])
	require.True(t, names["bonus-tag"])
	require.False(t, names[unusedTag.Name])
}

// --- helpers ---

func mustSubsite(t *testing.T, s *store.Store, slug, name string) store.FaqSubsites {
	t.Helper()
	sub, err := s.CreateSubsite(context.Background(), store.CreateSubsiteParams{
		Slug:        slug,
		DisplayName: name,
		Description: "",
	})
	require.NoError(t, err)
	return sub
}

func mustTag(t *testing.T, s *store.Store, name string) store.FaqTags {
	t.Helper()
	tag, err := s.GetOrCreateTag(context.Background(), store.GetOrCreateTagParams{
		Name: name,
	})
	require.NoError(t, err)
	return tag
}

// mustImportedQuestion inserts an imported question keyed to the given
// source. Each call uses a unique source_post_id within the source so
// repeated invocations don't trip the ON CONFLICT path.
func mustImportedQuestion(t *testing.T, s *store.Store, src store.FaqSources, title string) store.FaqQuestions {
	t.Helper()
	importedPostCounter++
	postID := importedPostCounter
	url := "https://example.test/q/" + title
	q, err := s.CreateImportedQuestion(context.Background(), store.CreateImportedQuestionParams{
		Title:        title,
		BodyMd:       "Imported body",
		BodyHtml:     "<p>Imported body</p>",
		SourceID:     &src.ID,
		SourcePostID: &postID,
		SourceUrl:    &url,
		NeedsReview:  false,
		CreatedAt: pgtype.Timestamptz{
			Time:  time.Now().Add(-365 * 24 * time.Hour),
			Valid: true,
		},
	})
	require.NoError(t, err)
	return q
}

var importedPostCounter int64 = 1000
