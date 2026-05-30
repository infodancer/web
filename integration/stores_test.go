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

// TestContactSubmissionsStore exercises the contact store against real Postgres:
// Save populates the row, Pending reflects pending/failed status, and the
// MarkSent / MarkFailed transitions behave.
func TestContactSubmissionsStore(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	if err := submissions.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mustExec(t, pool, "TRUNCATE contact_submissions RESTART IDENTITY")
	store := submissions.New(pool)

	sub := &submissions.Submission{
		Name: "Ada Lovelace", Email: "ada@example.org",
		Subject: "a question", Message: "please reply",
		IP: "203.0.113.7", UserAgent: "test",
	}
	if err := store.Save(ctx, sub); err != nil {
		t.Fatalf("save: %v", err)
	}
	if sub.ID == 0 {
		t.Error("Save did not populate ID")
	}
	if sub.CreatedAt.IsZero() {
		t.Error("Save did not populate CreatedAt")
	}

	pending, err := store.Pending(ctx, 5, 10)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 || pending[0].Email != "ada@example.org" {
		t.Fatalf("pending after save: got %d rows %+v, want 1 (ada)", len(pending), pending)
	}

	if err := store.MarkSent(ctx, sub.ID); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	if pending, _ = store.Pending(ctx, 5, 10); len(pending) != 0 {
		t.Errorf("pending after MarkSent: got %d, want 0", len(pending))
	}

	// A failed delivery stays pending (for the retry worker) until it exceeds
	// maxAttempts.
	fail := &submissions.Submission{Name: "Bot", Email: "b@example.org", Message: "hi"}
	if err := store.Save(ctx, fail); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	if err := store.MarkFailed(ctx, fail.ID, "smtp 550"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	pending, _ = store.Pending(ctx, 5, 10)
	if len(pending) != 1 || pending[0].ID != fail.ID {
		t.Errorf("pending after MarkFailed: got %d rows, want the failed one", len(pending))
	}
	// attempts incremented, so it drops out once maxAttempts is reached.
	if pending, _ = store.Pending(ctx, 1, 10); len(pending) != 0 {
		t.Errorf("pending with maxAttempts=1 after one failure: got %d, want 0", len(pending))
	}
}

// TestNewsletterSubscribersStore exercises the newsletter store: Insert reports
// new vs. duplicate, and MarkSynced succeeds.
func TestNewsletterSubscribersStore(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	if err := subscribers.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mustExec(t, pool, "TRUNCATE subscribers RESTART IDENTITY")
	store := subscribers.New(pool)

	isNew, err := store.Insert("reader@example.org")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !isNew {
		t.Error("first Insert: got isNew=false, want true")
	}

	isNew, err = store.Insert("reader@example.org")
	if err != nil {
		t.Fatalf("insert dup: %v", err)
	}
	if isNew {
		t.Error("duplicate Insert: got isNew=true, want false")
	}

	if err := store.MarkSynced("reader@example.org"); err != nil {
		t.Fatalf("mark synced: %v", err)
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
