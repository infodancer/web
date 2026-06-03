package integration

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/infodancer/web/contact/submissions"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newContactStore migrates the contact schema, truncates the table for a clean
// slate, and returns a ready store. Each test starts from an empty table so the
// subtests are order-independent.
func newContactStore(t *testing.T) (context.Context, *pgxpool.Pool, *submissions.Store) {
	t.Helper()
	ctx := context.Background()
	pool := newTestPool(t)
	if err := submissions.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mustExec(t, pool, "TRUNCATE contact_submissions RESTART IDENTITY")
	return ctx, pool, submissions.New(pool)
}

func TestContactSaveLifecycle(t *testing.T) {
	ctx, pool, store := newContactStore(t)

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

	// A freshly saved submission starts pending and visible to the worker.
	status, attempts, lastErr, sentAt := contactRow(t, ctx, pool, sub.ID)
	if status != submissions.StatusPending {
		t.Errorf("new submission status: got %q, want %q", status, submissions.StatusPending)
	}
	if attempts != 0 {
		t.Errorf("new submission attempts: got %d, want 0", attempts)
	}
	if lastErr != "" {
		t.Errorf("new submission last_error: got %q, want empty", lastErr)
	}
	if sentAt != nil {
		t.Errorf("new submission sent_at: got %v, want NULL", sentAt)
	}
}

// TestContactSaveDefaults confirms the NOT NULL DEFAULT ” columns accept a
// submission with the optional fields left empty.
func TestContactSaveDefaults(t *testing.T) {
	ctx, _, store := newContactStore(t)

	sub := &submissions.Submission{Email: "min@example.org", Message: "hi"}
	if err := store.Save(ctx, sub); err != nil {
		t.Fatalf("save minimal: %v", err)
	}
	if sub.ID == 0 {
		t.Error("Save did not populate ID for minimal submission")
	}
}

// TestContactPendingOrderAndLimit checks that PendingSubmissions returns rows in
// id ASC order and honors row_limit.
func TestContactPendingOrderAndLimit(t *testing.T) {
	ctx, _, store := newContactStore(t)

	const n = 5
	for i := range n {
		s := &submissions.Submission{Email: "u@example.org", Message: "m"}
		if err := store.Save(ctx, s); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	// Limit caps the result and the rows come back lowest-id first.
	got, err := store.Pending(ctx, 5, 2)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("pending with limit 2: got %d rows, want 2", len(got))
	}
	if got[0].ID >= got[1].ID {
		t.Errorf("pending not ordered by id ASC: %d then %d", got[0].ID, got[1].ID)
	}

	// A larger limit returns everything.
	all, err := store.Pending(ctx, 5, 100)
	if err != nil {
		t.Fatalf("pending all: %v", err)
	}
	if len(all) != n {
		t.Fatalf("pending all: got %d rows, want %d", len(all), n)
	}

	// A zero limit returns nothing.
	none, err := store.Pending(ctx, 5, 0)
	if err != nil {
		t.Fatalf("pending limit 0: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("pending with limit 0: got %d rows, want 0", len(none))
	}
}

// TestContactPendingStatusFilter checks that only pending and failed rows are
// returned: a sent row is excluded, a failed row stays visible.
func TestContactPendingStatusFilter(t *testing.T) {
	ctx, _, store := newContactStore(t)

	pending := &submissions.Submission{Email: "pending@example.org", Message: "m"}
	sent := &submissions.Submission{Email: "sent@example.org", Message: "m"}
	failed := &submissions.Submission{Email: "failed@example.org", Message: "m"}
	for _, s := range []*submissions.Submission{pending, sent, failed} {
		if err := store.Save(ctx, s); err != nil {
			t.Fatalf("save %s: %v", s.Email, err)
		}
	}
	if err := store.MarkSent(ctx, sent.ID); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	if err := store.MarkFailed(ctx, failed.ID, "smtp 550"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	got, err := store.Pending(ctx, 5, 100)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	gotEmails := map[string]bool{}
	for _, s := range got {
		gotEmails[s.Email] = true
	}
	if !gotEmails["pending@example.org"] {
		t.Error("pending row missing from results")
	}
	if !gotEmails["failed@example.org"] {
		t.Error("failed row should remain pending for retry")
	}
	if gotEmails["sent@example.org"] {
		t.Error("sent row should be excluded from pending")
	}
}

// TestContactMarkSent verifies MarkSent records the status, a sent_at timestamp,
// and increments the attempt counter.
func TestContactMarkSent(t *testing.T) {
	ctx, pool, store := newContactStore(t)

	sub := &submissions.Submission{Email: "s@example.org", Message: "m"}
	if err := store.Save(ctx, sub); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := store.MarkSent(ctx, sub.ID); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	status, attempts, _, sentAt := contactRow(t, ctx, pool, sub.ID)
	if status != submissions.StatusSent {
		t.Errorf("status after MarkSent: got %q, want %q", status, submissions.StatusSent)
	}
	if attempts != 1 {
		t.Errorf("attempts after MarkSent: got %d, want 1", attempts)
	}
	if sentAt == nil {
		t.Error("sent_at not set after MarkSent")
	}
}

// TestContactMarkFailed verifies a failed delivery stays pending, records the
// error, and increments attempts cumulatively until it exceeds max_attempts.
func TestContactMarkFailed(t *testing.T) {
	ctx, pool, store := newContactStore(t)

	sub := &submissions.Submission{Email: "f@example.org", Message: "m"}
	if err := store.Save(ctx, sub); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := store.MarkFailed(ctx, sub.ID, "smtp 550"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	status, attempts, lastErr, _ := contactRow(t, ctx, pool, sub.ID)
	if status != submissions.StatusFailed {
		t.Errorf("status after MarkFailed: got %q, want %q", status, submissions.StatusFailed)
	}
	if attempts != 1 {
		t.Errorf("attempts after one failure: got %d, want 1", attempts)
	}
	if lastErr != "smtp 550" {
		t.Errorf("last_error: got %q, want %q", lastErr, "smtp 550")
	}

	// Still visible with room for more attempts.
	if got, _ := store.Pending(ctx, 5, 10); len(got) != 1 {
		t.Errorf("pending after one failure (maxAttempts 5): got %d, want 1", len(got))
	}

	// A second failure increments attempts and updates the error.
	if err := store.MarkFailed(ctx, sub.ID, "smtp 421"); err != nil {
		t.Fatalf("mark failed 2: %v", err)
	}
	_, attempts, lastErr, _ = contactRow(t, ctx, pool, sub.ID)
	if attempts != 2 {
		t.Errorf("attempts after two failures: got %d, want 2", attempts)
	}
	if lastErr != "smtp 421" {
		t.Errorf("last_error after second failure: got %q, want %q", lastErr, "smtp 421")
	}

	// With max_attempts at 2 the row has reached the cap and drops out.
	if got, _ := store.Pending(ctx, 2, 10); len(got) != 0 {
		t.Errorf("pending with maxAttempts 2 after two failures: got %d, want 0", len(got))
	}
}

// TestContactMarkMissingIDNoOp confirms marking a nonexistent id is a harmless
// no-op rather than an error (the WHERE clause simply matches no rows).
func TestContactMarkMissingIDNoOp(t *testing.T) {
	ctx, _, store := newContactStore(t)

	if err := store.MarkSent(ctx, 999999); err != nil {
		t.Errorf("MarkSent on missing id: got error %v, want nil", err)
	}
	if err := store.MarkFailed(ctx, 999999, "nope"); err != nil {
		t.Errorf("MarkFailed on missing id: got error %v, want nil", err)
	}
}

// TestContactPendingRejectsOutOfRange checks the int32 narrowing guard rejects
// arguments that would overflow the DB column types.
func TestContactPendingRejectsOutOfRange(t *testing.T) {
	ctx, _, store := newContactStore(t)

	cases := []struct {
		name                  string
		maxAttempts, rowLimit int
	}{
		{"negative maxAttempts", -1, 10},
		{"negative limit", 5, -1},
		{"maxAttempts overflow", int(math.MaxInt32) + 1, 10},
		{"limit overflow", 5, int(math.MaxInt32) + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.Pending(ctx, tc.maxAttempts, tc.rowLimit); err == nil {
				t.Errorf("Pending(%d, %d): got nil error, want out-of-range error",
					tc.maxAttempts, tc.rowLimit)
			}
		})
	}
}

// contactRow reads the status, attempt count, last error, and sent_at for a
// submission directly, so tests can assert on columns the store does not expose.
func contactRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int64) (status string, attempts int, lastErr string, sentAt *time.Time) {
	t.Helper()
	err := pool.QueryRow(ctx,
		"SELECT email_status, email_attempts, email_last_error, sent_at FROM contact_submissions WHERE id = $1",
		id,
	).Scan(&status, &attempts, &lastErr, &sentAt)
	if err != nil {
		t.Fatalf("read contact row %d: %v", id, err)
	}
	return status, attempts, lastErr, sentAt
}
