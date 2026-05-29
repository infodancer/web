package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/infodancer/web/contact/submissions"
	"github.com/infodancer/web/contact/submissions/sqlite"
)

func TestSQLiteSaveAndStatus(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "contact.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	sub := &submissions.Submission{
		Name:    "Ada",
		Email:   "ada@example.org",
		Subject: "hello",
		Message: "hi",
		IP:      "127.0.0.1",
	}
	if err := store.Save(ctx, sub); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if sub.ID == 0 {
		t.Fatal("Save did not populate ID")
	}
	if sub.CreatedAt.IsZero() {
		t.Fatal("Save did not populate CreatedAt")
	}

	pending, err := store.Pending(ctx, 5, 10)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("Pending count: got %d want 1", len(pending))
	}
	if pending[0].Email != sub.Email {
		t.Errorf("Pending email: got %q want %q", pending[0].Email, sub.Email)
	}

	if err := store.MarkSent(ctx, sub.ID); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	pending, err = store.Pending(ctx, 5, 10)
	if err != nil {
		t.Fatalf("Pending after sent: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("Pending after sent: got %d want 0", len(pending))
	}
}

func TestSQLiteMarkFailedThenResend(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "contact.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	sub := &submissions.Submission{
		Name: "Ada", Email: "ada@example.org", Message: "hi",
	}
	if err := store.Save(ctx, sub); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.MarkFailed(ctx, sub.ID, "smtp timeout"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	// Failed rows still count as pending for the retry worker as long as
	// attempts are below the cap.
	pending, err := store.Pending(ctx, 5, 10)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("Pending after failure: got %d want 1", len(pending))
	}

	// Once attempts >= cap, the row drops out of the pending set.
	if err := store.MarkFailed(ctx, sub.ID, "smtp timeout 2"); err != nil {
		t.Fatalf("MarkFailed 2: %v", err)
	}
	pending, err = store.Pending(ctx, 2, 10)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("Pending after attempts cap: got %d want 0", len(pending))
	}
}
