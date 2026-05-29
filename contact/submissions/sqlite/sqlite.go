// Package sqlite provides the SQLite single-binary constructor for the
// contact submissions store. Importing it registers the modernc.org/sqlite
// driver, so only deployments that actually use SQLite pull that dependency
// in; Postgres consumers import the parent submissions package, which stays
// driver-agnostic.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/infodancer/web/contact/submissions"
	_ "modernc.org/sqlite"
)

// Open opens (or creates) a SQLite database at path, applies the submissions
// migrations, and returns a Store ready to use. The caller must Close() when
// done.
//
// For Postgres, open the *sql.DB yourself, call submissions.MigratePostgres,
// and pass it to submissions.New.
func Open(path string) (*submissions.Store, error) {
	if path == "" {
		return nil, errors.New("submissions: empty db path")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer.

	if err := submissions.MigrateSQLite(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return submissions.New(db, submissions.SQLite), nil
}
