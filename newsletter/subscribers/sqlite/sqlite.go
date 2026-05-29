// Package sqlite provides the SQLite single-binary constructor for the
// newsletter subscriber store. Importing it registers the modernc.org/sqlite
// driver, so only deployments that actually use SQLite pull that dependency
// in; Postgres consumers import the parent subscribers package, which stays
// driver-agnostic.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	sqlitemig "github.com/infodancer/web/newsletter/migrations/sqlite"
	"github.com/infodancer/web/newsletter/subscribers"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// Open opens (or creates) an SQLite database at path, applies migrations, and
// returns a Store. The caller must Close() the Store when done.
func Open(path string) (*subscribers.Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite: single writer

	provider, err := goose.NewProvider(goose.DialectSQLite3, db, sqlitemig.FS)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("goose provider: %w", err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("goose up: %w", err)
	}

	return subscribers.New(db, subscribers.SQLite), nil
}
