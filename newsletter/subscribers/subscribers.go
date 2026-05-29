// Package subscribers manages a local store of newsletter subscribers.
package subscribers

import (
	"database/sql"
	"fmt"
)

// Dialect holds database-specific query strings.
type Dialect struct {
	insert   string
	markSync string
}

// Postgres is the dialect for PostgreSQL databases.
var Postgres = Dialect{
	insert:   `INSERT INTO subscribers (email) VALUES ($1) ON CONFLICT DO NOTHING`,
	markSync: `UPDATE subscribers SET synced_at = NOW() WHERE email = $1`,
}

// SQLite is the dialect for SQLite databases.
var SQLite = Dialect{
	insert:   `INSERT OR IGNORE INTO subscribers (email) VALUES (?)`,
	markSync: `UPDATE subscribers SET synced_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE email = ?`,
}

// Store wraps a *sql.DB with subscriber operations.
type Store struct {
	db      *sql.DB
	dialect Dialect
}

// New creates a subscriber Store for an existing database connection.
// Schema must already be applied via goose migrations. For the SQLite
// single-binary case, the subscribers/sqlite subpackage's Open does both.
func New(db *sql.DB, d Dialect) *Store {
	return &Store{db: db, dialect: d}
}

// Close closes the underlying database connection.
// Only call this if the Store was created via the sqlite subpackage's Open;
// if you used New, the caller owns the connection and should close it
// directly.
func (s *Store) Close() error {
	return s.db.Close()
}

// Insert adds email to the subscribers table.
// Returns true if the address was new, false if already present.
func (s *Store) Insert(email string) (bool, error) {
	res, err := s.db.Exec(s.dialect.insert, email)
	if err != nil {
		return false, fmt.Errorf("insert: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkSynced records the current UTC time as the sync timestamp for email.
func (s *Store) MarkSynced(email string) error {
	_, err := s.db.Exec(s.dialect.markSync, email)
	return err
}
