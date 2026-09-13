package handler_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/faquser"
	"github.com/infodancer/web/faq/internal/handler"
	"github.com/infodancer/web/faq/internal/render"
	"github.com/infodancer/web/faq/store"
	"github.com/infodancer/web/pgtest"
)

var (
	testPool      *pgxpool.Pool
	testTemplates *render.Templates
)

func TestMain(m *testing.M) {
	// Load the embedded templates once. Since this file is outside the
	// faq package, we use the os filesystem rooted at the repo's
	// templates directory rather than the embed.FS in faq.go.
	tpls, err := render.New(os.DirFS("../.."), nil, uiassets.PartialsFS(), discardLogger())
	if err != nil {
		log.Fatalf("templates: %v", err)
	}
	testTemplates = tpls
	os.Exit(m.Run())
}

func runMigrations(_ context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(os.DirFS("../../migrations"))
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("up: %w", err)
	}
	return nil
}

// freshServer empties this test's database and returns a wired handler.Server
// plus the underlying store so tests can seed it directly.
func freshServer(t *testing.T, opts ...func(*handler.Deps)) (*handler.Server, *store.Store) {
	t.Helper()

	// This test's database from pgtest.Shared: created and migrated on the first
	// call, reused by a second freshServer in the same test -- which some tests
	// make, and then keep using the store from the first -- and emptied on every
	// call. testPool is package level so the tests that query it directly need no
	// change, which is sound only because no test in this package calls
	// t.Parallel.
	testPool = pgtest.Shared(t, pgtest.WithMigrate(runMigrations))

	_, err := testPool.Exec(context.Background(), `TRUNCATE
		faq_comments, faq_votes, faq_question_tags, faq_answers,
		faq_subsite_questions,
		faq_questions, faq_subsite_tags, faq_subsites,
		faq_tags, faq_imported_authors,
		faq_users, faq_sources
		RESTART IDENTITY CASCADE`)
	require.NoError(t, err)

	st := store.NewStore(testPool)
	deps := handler.Deps{
		Store:     st,
		Templates: testTemplates,
		Resolver:  anonResolver{},
		Logger:    discardLogger(),
		BasePath:  "/faq",
		PageSize:  25,
	}
	for _, opt := range opts {
		opt(&deps)
	}
	srv, err := handler.New(deps)
	require.NoError(t, err)
	return srv, st
}

// anonResolver returns (nil, nil) for every request — the read-path
// default.
type anonResolver struct{}

func (anonResolver) Resolve(_ context.Context, _ *http.Request) (*faquser.User, error) {
	return nil, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// pgDate is a tiny helper for seed rows that need a DATE column.
func pgDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// withStderrLogger swaps the test's discardLogger for one that writes
// to stderr. Use sparingly — bare freshServer should stay quiet so test
// output isn't polluted; pass this option when debugging a 500.
func withStderrLogger(t *testing.T) func(*handler.Deps) { //nolint:unused // documented debug helper, kept for diagnosing a 500 in tests
	t.Helper()
	return func(d *handler.Deps) {
		d.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
}
