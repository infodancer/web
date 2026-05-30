// Package subscribers manages a local store of newsletter subscribers,
// backed by PostgreSQL.
package subscribers

import (
	"context"
	"database/sql"
	"fmt"

	pgmig "github.com/infodancer/web/newsletter/migrations/postgres"
	"github.com/pressly/goose/v3"
)

const (
	insertSQL   = `INSERT INTO subscribers (email) VALUES ($1) ON CONFLICT DO NOTHING`
	markSyncSQL = `UPDATE subscribers SET synced_at = NOW() WHERE email = $1`
)

// Store wraps a *sql.DB with subscriber operations.
type Store struct {
	db *sql.DB
}

// New creates a subscriber Store over an existing PostgreSQL connection. The
// caller owns db (typically the host's shared pool) and should apply the
// schema first with Migrate; Store therefore has no Close.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Migrate applies the embedded PostgreSQL migrations to db.
func Migrate(ctx context.Context, db *sql.DB) error {
	p, err := goose.NewProvider(goose.DialectPostgres, db, pgmig.FS)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// Insert adds email to the subscribers table.
// Returns true if the address was new, false if already present.
func (s *Store) Insert(email string) (bool, error) {
	res, err := s.db.Exec(insertSQL, email)
	if err != nil {
		return false, fmt.Errorf("insert: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkSynced records the current UTC time as the sync timestamp for email.
func (s *Store) MarkSynced(email string) error {
	_, err := s.db.Exec(markSyncSQL, email)
	return err
}
