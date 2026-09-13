package integration

import (
	"context"
	"testing"

	"github.com/infodancer/web/contact/submissions"
	"github.com/infodancer/web/newsletter/subscribers"
	"github.com/infodancer/web/pgtest"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPool gives the test its own empty database, from pgtest, dropped when
// the test ends; the test skips when WEB_TEST_PG is unset. Several components
// migrate into that one database, which is the point of this suite: they share
// a database without colliding.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return pgtest.Pool(t)
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
