// Package submissions persists contact-form submissions and tracks
// the email-delivery status of each one.
package submissions

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	pgmig "github.com/infodancer/web/contact/migrations/postgres"
	sqlitemig "github.com/infodancer/web/contact/migrations/sqlite"
	"github.com/pressly/goose/v3"
)

// Submission represents a single contact-form message.
type Submission struct {
	ID        int64
	Name      string
	Email     string
	Subject   string
	Message   string
	IP        string
	UserAgent string
	CreatedAt time.Time
}

// Status values stored in the email_status column.
const (
	StatusPending = "pending"
	StatusSent    = "sent"
	StatusFailed  = "failed"
)

// Dialect carries the SQL fragments for one backend.
type Dialect struct {
	Kind     string // "postgres" or "sqlite"
	insert   string
	markSent string
	markFail string
	pending  string
}

// Postgres dialect.
var Postgres = Dialect{
	Kind: "postgres",
	insert: `INSERT INTO contact_submissions
		(name, email, subject, message, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`,
	markSent: `UPDATE contact_submissions
		SET email_status = 'sent',
		    sent_at = NOW(),
		    email_attempts = email_attempts + 1
		WHERE id = $1`,
	markFail: `UPDATE contact_submissions
		SET email_status = 'failed',
		    email_attempts = email_attempts + 1,
		    email_last_error = $2
		WHERE id = $1`,
	pending: `SELECT id, name, email, subject, message, ip, user_agent, created_at
		FROM contact_submissions
		WHERE email_status IN ('pending', 'failed')
		  AND email_attempts < $1
		ORDER BY id ASC
		LIMIT $2`,
}

// SQLite dialect.
var SQLite = Dialect{
	Kind: "sqlite",
	insert: `INSERT INTO contact_submissions
		(name, email, subject, message, ip, user_agent)
		VALUES (?, ?, ?, ?, ?, ?)`,
	markSent: `UPDATE contact_submissions
		SET email_status = 'sent',
		    sent_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
		    email_attempts = email_attempts + 1
		WHERE id = ?`,
	markFail: `UPDATE contact_submissions
		SET email_status = 'failed',
		    email_attempts = email_attempts + 1,
		    email_last_error = ?2
		WHERE id = ?1`,
	pending: `SELECT id, name, email, subject, message, ip, user_agent, created_at
		FROM contact_submissions
		WHERE email_status IN ('pending', 'failed')
		  AND email_attempts < ?
		ORDER BY id ASC
		LIMIT ?`,
}

// Store wraps a *sql.DB with submission operations.
type Store struct {
	db      *sql.DB
	dialect Dialect
}

// New creates a Store from an existing *sql.DB. The schema must already be
// applied (call MigrateSQLite or MigratePostgres first). For the SQLite
// single-binary case, the submissions/sqlite subpackage's Open does both.
func New(db *sql.DB, d Dialect) *Store {
	return &Store{db: db, dialect: d}
}

// MigrateSQLite applies the embedded SQLite migrations. The caller owns db
// and its driver registration (typically modernc.org/sqlite, registered by
// importing the submissions/sqlite subpackage).
func MigrateSQLite(ctx context.Context, db *sql.DB) error {
	p, err := goose.NewProvider(goose.DialectSQLite3, db, sqlitemig.FS)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// MigratePostgres applies the embedded PostgreSQL migrations.
func MigratePostgres(ctx context.Context, db *sql.DB) error {
	p, err := goose.NewProvider(goose.DialectPostgres, db, pgmig.FS)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// Close closes the underlying database connection. Only valid for stores
// created by Open; if you used New, the caller owns the connection.
func (s *Store) Close() error { return s.db.Close() }

// Save inserts a new submission and populates ID and CreatedAt on success.
func (s *Store) Save(ctx context.Context, sub *Submission) error {
	switch s.dialect.Kind {
	case "postgres":
		return s.db.QueryRowContext(ctx, s.dialect.insert,
			sub.Name, sub.Email, sub.Subject, sub.Message, sub.IP, sub.UserAgent,
		).Scan(&sub.ID, &sub.CreatedAt)
	case "sqlite":
		res, err := s.db.ExecContext(ctx, s.dialect.insert,
			sub.Name, sub.Email, sub.Subject, sub.Message, sub.IP, sub.UserAgent,
		)
		if err != nil {
			return fmt.Errorf("insert: %w", err)
		}
		sub.ID, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("last insert id: %w", err)
		}
		sub.CreatedAt = time.Now().UTC()
		return nil
	default:
		return fmt.Errorf("submissions: unknown dialect kind %q", s.dialect.Kind)
	}
}

// MarkSent records a successful delivery.
func (s *Store) MarkSent(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, s.dialect.markSent, id)
	return err
}

// MarkFailed records a failed delivery attempt with the error message.
func (s *Store) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	_, err := s.db.ExecContext(ctx, s.dialect.markFail, id, errMsg)
	return err
}

// Pending returns up to limit submissions whose email_attempts is below
// maxAttempts and whose status is pending or failed. Used by retry workers
// (slated for v0.2 — exposed now so the worker can be wired in without an
// API change).
func (s *Store) Pending(ctx context.Context, maxAttempts, limit int) ([]Submission, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.pending, maxAttempts, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []Submission
	for rows.Next() {
		var (
			sub  Submission
			when timeScanner
		)
		if err := rows.Scan(&sub.ID, &sub.Name, &sub.Email, &sub.Subject,
			&sub.Message, &sub.IP, &sub.UserAgent, &when); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		sub.CreatedAt = when.t
		out = append(out, sub)
	}
	return out, rows.Err()
}

// timeScanner accepts both native time.Time (Postgres) and ISO-8601 strings
// (SQLite TEXT columns) when scanning a row.
type timeScanner struct{ t time.Time }

func (s *timeScanner) Scan(src any) error {
	switch v := src.(type) {
	case time.Time:
		s.t = v
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return fmt.Errorf("parse time %q: %w", v, err)
		}
		s.t = t
	case []byte:
		t, err := time.Parse(time.RFC3339, string(v))
		if err != nil {
			return fmt.Errorf("parse time %q: %w", v, err)
		}
		s.t = t
	case nil:
		// leave zero value
	default:
		return fmt.Errorf("unexpected time scan type %T", src)
	}
	return nil
}
