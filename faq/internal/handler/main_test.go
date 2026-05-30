//go:build integration

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
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/faquser"
	"github.com/infodancer/web/faq/internal/handler"
	"github.com/infodancer/web/faq/internal/render"
	"github.com/infodancer/web/faq/store"
)

var (
	testPool      *pgxpool.Pool
	testTemplates *render.Templates
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	pg, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("faq_handler_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Fatalf("postgres container: %v", err)
	}

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("connection string: %v", err)
	}

	testPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("pool: %v", err)
	}

	if err := runMigrations(testPool); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	// Load the embedded templates once. Since this file is outside the
	// faq package, we use the os filesystem rooted at the repo's
	// templates directory rather than the embed.FS in faq.go.
	tpls, err := render.New(os.DirFS("../.."), nil, uiassets.PartialsFS(), discardLogger())
	if err != nil {
		log.Fatalf("templates: %v", err)
	}
	testTemplates = tpls

	code := m.Run()
	testPool.Close()
	_ = pg.Terminate(ctx)
	os.Exit(code)
}

func runMigrations(pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

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

// freshServer truncates the faq_* tables and returns a wired handler.Server
// plus the underlying store so tests can seed it directly.
func freshServer(t *testing.T, opts ...func(*handler.Deps)) (*handler.Server, *store.Store) {
	t.Helper()
	ctx := context.Background()

	_, err := testPool.Exec(ctx, `TRUNCATE
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
func withStderrLogger(t *testing.T) func(*handler.Deps) {
	t.Helper()
	return func(d *handler.Deps) {
		d.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
}
