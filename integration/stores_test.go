package integration

import (
	"context"
	"os"
	"testing"

	"github.com/infodancer/web/contact/submissions"
	"github.com/infodancer/web/newsletter/subscribers"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPool connects to the Postgres named by TEST_DATABASE_URL. The suite is
// skipped when it is unset, so a plain `go test` without a database passes; CI
// sets it to the workflow's Postgres service.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// TestComponentsShareOneDatabase is the point of this module: several components
// migrate into the *same* database without colliding, because each prefixes its
// tables and tracks migrations in its own goose version table.
func TestComponentsShareOneDatabase(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	if err := submissions.Migrate(ctx, pool); err != nil {
		t.Fatalf("contact migrate: %v", err)
	}
	if err := subscribers.Migrate(ctx, pool); err != nil {
		t.Fatalf("newsletter migrate: %v", err)
	}

	for _, table := range []string{
		"contact_submissions",
		"subscribers",
		"contact_goose_db_version",
		"newsletter_goose_db_version",
	} {
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = $1)",
			table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %q to exist in the shared database", table)
		}
	}
}
