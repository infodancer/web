package integration

import (
	"context"
	"testing"
	"time"

	"github.com/infodancer/web/newsletter/subscribers"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newSubscriberStore migrates the newsletter schema, truncates the table, and
// returns a ready store so each test starts from an empty table.
func newSubscriberStore(t *testing.T) (context.Context, *pgxpool.Pool, *subscribers.Store) {
	t.Helper()
	ctx := context.Background()
	pool := newTestPool(t)
	if err := subscribers.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mustExec(t, pool, "TRUNCATE subscribers RESTART IDENTITY")
	return ctx, pool, subscribers.New(pool)
}

// TestNewsletterInsert checks the ON CONFLICT DO NOTHING semantics: a new
// address reports isNew=true, a repeat reports false, and distinct addresses are
// independent.
func TestNewsletterInsert(t *testing.T) {
	_, pool, store := newSubscriberStore(t)

	isNew, err := store.Insert("reader@example.org")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !isNew {
		t.Error("first Insert: got isNew=false, want true")
	}

	// created_at is populated by the DEFAULT NOW().
	if createdAt := subscriberCreatedAt(t, pool, "reader@example.org"); createdAt.IsZero() {
		t.Error("created_at not populated after Insert")
	}

	isNew, err = store.Insert("reader@example.org")
	if err != nil {
		t.Fatalf("insert dup: %v", err)
	}
	if isNew {
		t.Error("duplicate Insert: got isNew=true, want false")
	}

	// A different address is its own row.
	isNew, err = store.Insert("other@example.org")
	if err != nil {
		t.Fatalf("insert second: %v", err)
	}
	if !isNew {
		t.Error("distinct Insert: got isNew=false, want true")
	}

	if n := subscriberCount(t, pool); n != 2 {
		t.Errorf("row count after two distinct + one duplicate insert: got %d, want 2", n)
	}
}

// TestNewsletterMarkSynced verifies MarkSynced stamps synced_at and that a
// duplicate insert after syncing still reports not-new (the conflict path does
// not reset the row).
func TestNewsletterMarkSynced(t *testing.T) {
	_, pool, store := newSubscriberStore(t)

	if _, err := store.Insert("reader@example.org"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if syncedAt := subscriberSyncedAt(t, pool, "reader@example.org"); syncedAt != nil {
		t.Errorf("synced_at before MarkSynced: got %v, want NULL", syncedAt)
	}

	if err := store.MarkSynced("reader@example.org"); err != nil {
		t.Fatalf("mark synced: %v", err)
	}
	if syncedAt := subscriberSyncedAt(t, pool, "reader@example.org"); syncedAt == nil {
		t.Error("synced_at not set after MarkSynced")
	}

	isNew, err := store.Insert("reader@example.org")
	if err != nil {
		t.Fatalf("insert after sync: %v", err)
	}
	if isNew {
		t.Error("Insert after MarkSynced: got isNew=true, want false")
	}
}

// TestNewsletterMarkSyncedMissingNoOp confirms syncing an unknown address is a
// harmless no-op (UPDATE matches no rows) rather than an error.
func TestNewsletterMarkSyncedMissingNoOp(t *testing.T) {
	_, _, store := newSubscriberStore(t)

	if err := store.MarkSynced("nobody@example.org"); err != nil {
		t.Errorf("MarkSynced on missing address: got error %v, want nil", err)
	}
}

func subscriberCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM subscribers").Scan(&n); err != nil {
		t.Fatalf("count subscribers: %v", err)
	}
	return n
}

func subscriberCreatedAt(t *testing.T, pool *pgxpool.Pool, email string) time.Time {
	t.Helper()
	var createdAt time.Time
	err := pool.QueryRow(context.Background(),
		"SELECT created_at FROM subscribers WHERE email = $1", email).Scan(&createdAt)
	if err != nil {
		t.Fatalf("read created_at for %s: %v", email, err)
	}
	return createdAt
}

func subscriberSyncedAt(t *testing.T, pool *pgxpool.Pool, email string) *time.Time {
	t.Helper()
	var syncedAt *time.Time
	err := pool.QueryRow(context.Background(),
		"SELECT synced_at FROM subscribers WHERE email = $1", email).Scan(&syncedAt)
	if err != nil {
		t.Fatalf("read synced_at for %s: %v", email, err)
	}
	return syncedAt
}
