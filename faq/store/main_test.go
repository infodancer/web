package store_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/infodancer/web/faq/store"
	"github.com/infodancer/web/pgtest"
)

// testPool is the current test's database, set by freshStore. It is package
// level so the tests that query it directly need no change, which is sound only
// because no test in this package calls t.Parallel: a parallel test would see
// another test's database.
var testPool *pgxpool.Pool

func runMigrations(_ context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(os.DirFS("../migrations"))
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("up: %w", err)
	}
	return nil
}

// freshStore returns a Store on this test's database, emptied. The database comes
// from pgtest.Shared: created and migrated on the first call, reused by later
// calls in the same test, dropped when the test ends, and skipped when
// WEB_TEST_PG is unset. Use at the top of every integration test.
func freshStore(t *testing.T) *store.Store {
	t.Helper()
	testPool = pgtest.Shared(t, pgtest.WithMigrate(runMigrations))

	_, err := testPool.Exec(context.Background(), `TRUNCATE
		faq_comments, faq_votes, faq_question_tags, faq_answers,
		faq_questions, faq_subsite_tags, faq_subsites,
		faq_tags, faq_imported_authors,
		faq_users, faq_sources
		RESTART IDENTITY CASCADE`)
	require.NoError(t, err)

	return store.NewStore(testPool)
}

// subHash returns the canonical 32-byte sub_hash for an issuer + subject pair.
// Matches what the runtime middleware will compute when it sees a JWT.
func subHash(issuer, subject string) []byte {
	h := sha256.Sum256([]byte(issuer + ":" + subject))
	return h[:]
}

// mustUser upserts a user and returns it. Convenience for tests.
func mustUser(t *testing.T, s *store.Store, name string) store.FaqUsers {
	t.Helper()
	u, err := s.UpsertUser(context.Background(), store.UpsertUserParams{
		SubHash:     subHash("https://test.example", name),
		DisplayName: name,
	})
	require.NoError(t, err)
	return u
}

// mustSource upserts a faq_sources row. The dump_date defaults to 2023-09-01
// to match the v0.1 import cutoff policy.
func mustSource(t *testing.T, s *store.Store, domain string) store.FaqSources {
	t.Helper()
	src, err := s.UpsertSource(context.Background(), store.UpsertSourceParams{
		SourceDomain: domain,
		DisplayName:  domain,
		License:      "CC BY-SA 4.0",
		DumpDate:     pgDate(2023, 9, 1),
		PostCount:    0,
	})
	require.NoError(t, err)
	return src
}

// mustQuestion creates a native (status='open') question.
func mustQuestion(t *testing.T, s *store.Store, author store.FaqUsers, title string) store.FaqQuestions {
	t.Helper()
	q, err := s.CreateQuestion(context.Background(), store.CreateQuestionParams{
		Title:        title,
		BodyMd:       "Body of " + title,
		BodyHtml:     "<p>Body of " + title + "</p>",
		AuthorUserID: &author.ID,
	})
	require.NoError(t, err)
	return q
}

// mustAnswer creates a native answer to q.
func mustAnswer(t *testing.T, s *store.Store, q store.FaqQuestions, author store.FaqUsers, body string) store.FaqAnswers {
	t.Helper()
	a, err := s.CreateAnswer(context.Background(), store.CreateAnswerParams{
		QuestionID:   q.ID,
		BodyMd:       body,
		BodyHtml:     "<p>" + body + "</p>",
		AuthorUserID: &author.ID,
	})
	require.NoError(t, err)
	return a
}

// pgDate wraps a Go date as the pgtype.Date sqlc emits for DATE columns.
func pgDate(year int, month time.Month, day int) pgtype.Date {
	return pgtype.Date{
		Time:  time.Date(year, month, day, 0, 0, 0, 0, time.UTC),
		Valid: true,
	}
}
