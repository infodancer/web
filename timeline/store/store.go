// Package store is the high-level data-access entry point for the timeline
// module. It wraps a pgx connection pool over the sqlc-generated db package,
// exposing every query method directly via the embedded [*db.Queries] pointer
// and adding a [Store.WithTx] helper for transactional sequences.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/timeline/db"
)

// Store wraps a pgx pool and the generated query layer. A zero Store is
// unusable; construct one with [NewStore].
type Store struct {
	*db.Queries
	pool *pgxpool.Pool
}

// NewStore constructs a Store from a pgx connection pool owned by the host.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{Queries: db.New(pool), pool: pool}
}

// Pool returns the underlying connection pool. Prefer the query methods or
// [Store.WithTx]; provided for tests and ingestion tooling.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// WithTx runs fn inside a database transaction. On any error returned by fn (or
// a panic), the transaction is rolled back; on nil return it is committed. The
// [*db.Queries] passed to fn is bound to the transaction.
func (s *Store) WithTx(ctx context.Context, fn func(*db.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(s.Queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
