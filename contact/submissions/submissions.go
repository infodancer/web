// Package submissions persists contact-form submissions and tracks
// the email-delivery status of each one. It is backed by PostgreSQL.
package submissions

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	pgmig "github.com/infodancer/web/contact/migrations/postgres"
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

const (
	insertSQL = `INSERT INTO contact_submissions
		(name, email, subject, message, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`
	markSentSQL = `UPDATE contact_submissions
		SET email_status = 'sent',
		    sent_at = NOW(),
		    email_attempts = email_attempts + 1
		WHERE id = $1`
	markFailSQL = `UPDATE contact_submissions
		SET email_status = 'failed',
		    email_attempts = email_attempts + 1,
		    email_last_error = $2
		WHERE id = $1`
	pendingSQL = `SELECT id, name, email, subject, message, ip, user_agent, created_at
		FROM contact_submissions
		WHERE email_status IN ('pending', 'failed')
		  AND email_attempts < $1
		ORDER BY id ASC
		LIMIT $2`
)

// Store wraps a *sql.DB with submission operations.
type Store struct {
	db *sql.DB
}

// New creates a Store over an existing PostgreSQL connection. The caller owns
// db and should apply the schema first with Migrate. Because the caller owns
// the connection (typically the host's shared pool), Store has no Close.
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

// Save inserts a new submission and populates ID and CreatedAt on success.
func (s *Store) Save(ctx context.Context, sub *Submission) error {
	return s.db.QueryRowContext(ctx, insertSQL,
		sub.Name, sub.Email, sub.Subject, sub.Message, sub.IP, sub.UserAgent,
	).Scan(&sub.ID, &sub.CreatedAt)
}

// MarkSent records a successful delivery.
func (s *Store) MarkSent(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, markSentSQL, id)
	return err
}

// MarkFailed records a failed delivery attempt with the error message.
func (s *Store) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	_, err := s.db.ExecContext(ctx, markFailSQL, id, errMsg)
	return err
}

// Pending returns up to limit submissions whose email_attempts is below
// maxAttempts and whose status is pending or failed. Used by retry workers
// (slated for v0.2 — exposed now so the worker can be wired in without an
// API change).
func (s *Store) Pending(ctx context.Context, maxAttempts, limit int) ([]Submission, error) {
	rows, err := s.db.QueryContext(ctx, pendingSQL, maxAttempts, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []Submission
	for rows.Next() {
		var sub Submission
		if err := rows.Scan(&sub.ID, &sub.Name, &sub.Email, &sub.Subject,
			&sub.Message, &sub.IP, &sub.UserAgent, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
