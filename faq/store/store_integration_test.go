//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
)

func TestUpsertSource_Idempotent(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	first, err := s.UpsertSource(ctx, store.UpsertSourceParams{
		SourceDomain: "qa.example.com",
		DisplayName:  "Example Q&A",
		License:      "CC BY-SA 4.0",
		DumpDate:     pgDate(2023, 9, 1),
		PostCount:    0,
	})
	require.NoError(t, err)

	second, err := s.UpsertSource(ctx, store.UpsertSourceParams{
		SourceDomain: "qa.example.com",
		DisplayName:  "Example Q&A (renamed)",
		License:      "CC BY-SA 4.0",
		DumpDate:     pgDate(2023, 9, 1),
		PostCount:    0,
	})
	require.NoError(t, err)

	require.Equal(t, first.ID, second.ID, "upsert should reuse id")
	require.Equal(t, "Example Q&A (renamed)", second.DisplayName, "upsert should update display_name on conflict")
}

func TestUpsertUser_UpdatesLastSeen(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	first := mustUser(t, s, "alice")
	time.Sleep(10 * time.Millisecond)

	second, err := s.UpsertUser(ctx, store.UpsertUserParams{
		SubHash:     subHash("https://test.example", "alice"),
		DisplayName: "Alice (renamed)",
	})
	require.NoError(t, err)

	require.Equal(t, first.ID, second.ID)
	require.Equal(t, "Alice (renamed)", second.DisplayName)
	require.True(t, second.LastSeenAt.Time.After(first.LastSeenAt.Time),
		"last_seen_at should advance: first=%v second=%v", first.LastSeenAt, second.LastSeenAt)
}

func TestGetOrCreateTag_Idempotent(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	first, err := s.GetOrCreateTag(ctx, store.GetOrCreateTagParams{
		Name: "world-building",
	})
	require.NoError(t, err)

	second, err := s.GetOrCreateTag(ctx, store.GetOrCreateTagParams{
		Name: "world-building",
	})
	require.NoError(t, err)

	require.Equal(t, first.ID, second.ID)
}

func TestCreateQuestion_DefaultsToOpen(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	q := mustQuestion(t, s, alice, "What is a barbarian?")

	require.Equal(t, store.FaqQuestionStatusOpen, q.Status,
		"native questions default to 'open'")
	require.Nil(t, q.SourceID, "native questions have no source")
	require.False(t, q.LocallyModified)
}

func TestCreateImportedQuestion_DefaultsToLocked(t *testing.T) {
	s := freshStore(t)
	src := mustSource(t, s, "qa.example.com")
	ctx := context.Background()

	srcPostID := int64(42)
	srcURL := "https://qa.example.com/q/42"

	q, err := s.CreateImportedQuestion(ctx, store.CreateImportedQuestionParams{
		Title:        "Imported question",
		BodyMd:       "Imported body",
		BodyHtml:     "<p>Imported body</p>",
		SourceID:     &src.ID,
		SourcePostID: &srcPostID,
		SourceUrl:    &srcURL,
		NeedsReview:  false,
		CreatedAt: pgtype.Timestamptz{
			Time:  time.Now().Add(-365 * 24 * time.Hour),
			Valid: true,
		},
	})
	require.NoError(t, err)

	require.Equal(t, store.FaqQuestionStatusLocked, q.Status,
		"imported questions default to 'locked' per §6.1")
}

func TestCreateImportedQuestion_IdempotentOnSourceKey(t *testing.T) {
	s := freshStore(t)
	src := mustSource(t, s, "qa.example.com")
	ctx := context.Background()

	srcPostID := int64(42)
	srcURL := "https://qa.example.com/q/42"
	created := pgtype.Timestamptz{Time: time.Now(), Valid: true}

	first, err := s.CreateImportedQuestion(ctx, store.CreateImportedQuestionParams{
		Title:        "v1 title",
		BodyMd:       "v1",
		BodyHtml:     "<p>v1</p>",
		SourceID:     &src.ID,
		SourcePostID: &srcPostID,
		SourceUrl:    &srcURL,
		CreatedAt:    created,
	})
	require.NoError(t, err)

	second, err := s.CreateImportedQuestion(ctx, store.CreateImportedQuestionParams{
		Title:        "v2 title",
		BodyMd:       "v2",
		BodyHtml:     "<p>v2</p>",
		SourceID:     &src.ID,
		SourcePostID: &srcPostID,
		SourceUrl:    &srcURL,
		CreatedAt:    created,
	})
	require.NoError(t, err)

	require.Equal(t, first.ID, second.ID, "upsert reuses id on (source_id, source_post_id)")
	require.Equal(t, "v2 title", second.Title)
	require.Equal(t, "v2", second.BodyMd)
}

func TestListAnswersForQuestion_SortedByTotalScoreDesc(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	bob := mustUser(t, s, "bob")
	carol := mustUser(t, s, "carol")
	q := mustQuestion(t, s, alice, "Q")

	lowAns := mustAnswer(t, s, q, bob, "low-score")
	highAns := mustAnswer(t, s, q, carol, "high-score")

	ctx := context.Background()
	_, err := s.UpsertVote(ctx, store.UpsertVoteParams{
		AnswerID: highAns.ID, UserID: alice.ID, Value: store.FaqVoteValueUp,
	})
	require.NoError(t, err)

	answers, err := s.ListAnswersForQuestion(ctx, store.ListAnswersForQuestionParams{QuestionID: q.ID})
	require.NoError(t, err)
	require.Len(t, answers, 2)
	require.Equal(t, highAns.ID, answers[0].ID, "highest score first")
	require.Equal(t, lowAns.ID, answers[1].ID)
	require.EqualValues(t, 1, answers[0].TotalScore)
	require.EqualValues(t, 0, answers[1].TotalScore)
}

// ---------------------------------------------------------------------------
// Vote trigger correctness
// ---------------------------------------------------------------------------

func TestVoteTrigger_UpIncrementsScore(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	bob := mustUser(t, s, "bob")
	q := mustQuestion(t, s, alice, "Q")
	a := mustAnswer(t, s, q, alice, "answer-by-alice")
	ctx := context.Background()

	_, err := s.UpsertVote(ctx, store.UpsertVoteParams{
		AnswerID: a.ID, UserID: bob.ID, Value: store.FaqVoteValueUp,
	})
	require.NoError(t, err)

	got, err := s.GetAnswer(ctx, a.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, got.LocalScore)
}

func TestVoteTrigger_DownDecrementsScore(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	bob := mustUser(t, s, "bob")
	q := mustQuestion(t, s, alice, "Q")
	a := mustAnswer(t, s, q, alice, "answer")
	ctx := context.Background()

	_, err := s.UpsertVote(ctx, store.UpsertVoteParams{
		AnswerID: a.ID, UserID: bob.ID, Value: store.FaqVoteValueDown,
	})
	require.NoError(t, err)

	got, err := s.GetAnswer(ctx, a.ID)
	require.NoError(t, err)
	require.EqualValues(t, -1, got.LocalScore)
}

func TestVoteTrigger_FlipApplies2pointDelta(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	bob := mustUser(t, s, "bob")
	q := mustQuestion(t, s, alice, "Q")
	a := mustAnswer(t, s, q, alice, "answer")
	ctx := context.Background()

	// up: score = 1
	_, err := s.UpsertVote(ctx, store.UpsertVoteParams{
		AnswerID: a.ID, UserID: bob.ID, Value: store.FaqVoteValueUp,
	})
	require.NoError(t, err)

	// flip to down: score should go 1 -> -1 (delta -2)
	_, err = s.UpsertVote(ctx, store.UpsertVoteParams{
		AnswerID: a.ID, UserID: bob.ID, Value: store.FaqVoteValueDown,
	})
	require.NoError(t, err)

	got, err := s.GetAnswer(ctx, a.ID)
	require.NoError(t, err)
	require.EqualValues(t, -1, got.LocalScore, "up->down delta is -2, score should be -1")
}

func TestVoteTrigger_DeleteReversesScore(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	bob := mustUser(t, s, "bob")
	q := mustQuestion(t, s, alice, "Q")
	a := mustAnswer(t, s, q, alice, "answer")
	ctx := context.Background()

	_, err := s.UpsertVote(ctx, store.UpsertVoteParams{
		AnswerID: a.ID, UserID: bob.ID, Value: store.FaqVoteValueUp,
	})
	require.NoError(t, err)

	err = s.DeleteVote(ctx, store.DeleteVoteParams{
		AnswerID: a.ID, UserID: bob.ID,
	})
	require.NoError(t, err)

	got, err := s.GetAnswer(ctx, a.ID)
	require.NoError(t, err)
	require.EqualValues(t, 0, got.LocalScore, "delete reverses the prior delta")
}

func TestVoteEligibility_SelfVoteRejected(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	q := mustQuestion(t, s, alice, "Q")
	a := mustAnswer(t, s, q, alice, "alice's own answer")
	ctx := context.Background()

	_, err := s.UpsertVote(ctx, store.UpsertVoteParams{
		AnswerID: a.ID, UserID: alice.ID, Value: store.FaqVoteValueUp,
	})
	require.Error(t, err, "self-vote should be rejected by trigger")
	require.Contains(t, err.Error(), "self-vote")
}

func TestVoteEligibility_LockedQuestionRejected(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	bob := mustUser(t, s, "bob")
	q := mustQuestion(t, s, alice, "Q")
	a := mustAnswer(t, s, q, alice, "answer")
	ctx := context.Background()

	// Lock the question.
	err := s.SetQuestionStatus(ctx, store.SetQuestionStatusParams{
		ID:     q.ID,
		Status: store.FaqQuestionStatusLocked,
	})
	require.NoError(t, err)

	_, err = s.UpsertVote(ctx, store.UpsertVoteParams{
		AnswerID: a.ID, UserID: bob.ID, Value: store.FaqVoteValueUp,
	})
	require.Error(t, err, "vote on locked question should be rejected by trigger")
	require.Contains(t, err.Error(), "non-open")
}

func TestVoteEligibility_OpenQuestionAccepted(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	bob := mustUser(t, s, "bob")
	q := mustQuestion(t, s, alice, "Q")
	a := mustAnswer(t, s, q, alice, "answer")
	ctx := context.Background()

	// Sanity: native question starts open.
	require.Equal(t, store.FaqQuestionStatusOpen, q.Status)

	_, err := s.UpsertVote(ctx, store.UpsertVoteParams{
		AnswerID: a.ID, UserID: bob.ID, Value: store.FaqVoteValueUp,
	})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Comments
// ---------------------------------------------------------------------------

func TestComment_CheckConstraintExactlyOneParent(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	q := mustQuestion(t, s, alice, "Q")
	a := mustAnswer(t, s, q, alice, "answer")
	ctx := context.Background()

	// Inserting a comment that names BOTH a question and an answer must fail
	// via the CHECK constraint. sqlc doesn't model this case in the typed
	// API, so we go through the raw pool.
	_, err := testPool.Exec(ctx, `
		INSERT INTO faq_comments (question_id, answer_id, body_md, body_html)
		VALUES ($1, $2, 'bad', '<p>bad</p>')`, q.ID, a.ID)
	require.Error(t, err, "comment with both parents must be rejected")
	require.Contains(t, err.Error(), "faq_comments_one_parent")

	// Inserting with NEITHER parent must also fail.
	_, err = testPool.Exec(ctx, `
		INSERT INTO faq_comments (body_md, body_html)
		VALUES ('bad', '<p>bad</p>')`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "faq_comments_one_parent")
}

func TestCreateQuestionComment_OK(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	q := mustQuestion(t, s, alice, "Q")
	ctx := context.Background()

	c, err := s.CreateQuestionComment(ctx, store.CreateQuestionCommentParams{
		QuestionID:   &q.ID,
		BodyMd:       "first comment",
		BodyHtml:     "<p>first comment</p>",
		AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, c.QuestionID)
	require.Equal(t, q.ID, *c.QuestionID)
	require.Nil(t, c.AnswerID)
}

// ---------------------------------------------------------------------------
// Search and sitemap
// ---------------------------------------------------------------------------

func TestSearch_TitleRanksAboveBody(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	ctx := context.Background()

	// title-hit question
	_, err := s.CreateQuestion(ctx, store.CreateQuestionParams{
		Title:        "How does fireball work in 5e?",
		BodyMd:       "Some unrelated body about armor.",
		BodyHtml:     "<p>Some unrelated body about armor.</p>",
		AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)

	// body-hit question (no title match)
	_, err = s.CreateQuestion(ctx, store.CreateQuestionParams{
		Title:        "What is initiative?",
		BodyMd:       "I cast fireball at the goblin and roll damage.",
		BodyHtml:     "<p>I cast fireball at the goblin and roll damage.</p>",
		AuthorUserID: &alice.ID,
	})
	require.NoError(t, err)

	rows, err := s.SearchQuestions(ctx, store.SearchQuestionsParams{
		SearchQuery: "fireball",
		LimitN:      10,
		OffsetN:     0,
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "How does fireball work in 5e?", rows[0].Title,
		"title match should rank above body match")
}

func TestListQuestionsForSitemap_StableByID(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	ctx := context.Background()

	// Insert 5 questions with descending titles so they wouldn't be ordered
	// by title or created_at-equivalent. Verify the sitemap query returns
	// them by id ASC regardless.
	for i := range 5 {
		_, err := s.CreateQuestion(ctx, store.CreateQuestionParams{
			Title:        "Q " + string(rune('e'-i)), // 'e','d','c','b','a'
			BodyMd:       "body",
			BodyHtml:     "<p>body</p>",
			AuthorUserID: &alice.ID,
		})
		require.NoError(t, err)
	}

	rows, err := s.ListQuestionsForSitemap(ctx, store.ListQuestionsForSitemapParams{
		LimitN:  10,
		OffsetN: 0,
	})
	require.NoError(t, err)
	require.Len(t, rows, 5)

	for i := 1; i < len(rows); i++ {
		require.Greater(t, rows[i].ID, rows[i-1].ID, "id must be strictly ascending")
	}
}

func TestSetQuestionStatus_Lock(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	q := mustQuestion(t, s, alice, "Q")
	ctx := context.Background()

	err := s.SetQuestionStatus(ctx, store.SetQuestionStatusParams{
		ID:     q.ID,
		Status: store.FaqQuestionStatusLocked,
	})
	require.NoError(t, err)

	got, err := s.GetQuestion(ctx, q.ID)
	require.NoError(t, err)
	require.Equal(t, store.FaqQuestionStatusLocked, got.Status)
	require.NotNil(t, got.ClosedAt)
}

func TestGetQuestion_HidesDeleted(t *testing.T) {
	s := freshStore(t)
	alice := mustUser(t, s, "alice")
	q := mustQuestion(t, s, alice, "Q")
	ctx := context.Background()

	err := s.SetQuestionStatus(ctx, store.SetQuestionStatusParams{
		ID:     q.ID,
		Status: store.FaqQuestionStatusDeleted,
	})
	require.NoError(t, err)

	_, err = s.GetQuestion(ctx, q.ID)
	require.Error(t, err, "GetQuestion filters status='deleted'")

	got, err := s.GetQuestionIncludingDeleted(ctx, q.ID)
	require.NoError(t, err)
	require.Equal(t, store.FaqQuestionStatusDeleted, got.Status)
}
