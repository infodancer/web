package sqlite_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/infodancer/web/newsletter/subscribers/sqlite"
)

func TestOpenAndInsert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	// First insert should be new
	isNew, err := store.Insert("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !isNew {
		t.Error("expected new subscriber")
	}

	// Duplicate should not be new
	isNew, err = store.Insert("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if isNew {
		t.Error("expected duplicate subscriber")
	}
}

func TestMarkSynced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	if _, err := store.Insert("bob@example.com"); err != nil {
		t.Fatal(err)
	}

	if err := store.MarkSynced("bob@example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "sub", "dir", "test.db")

	store, err := sqlite.Open(nested)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	if _, err := os.Stat(filepath.Dir(nested)); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
}
