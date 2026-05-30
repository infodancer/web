package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the high-level data-access entry point for the faq module. It
// wraps a pgx connection pool, exposes every sqlc-generated query method
// directly via the embedded [*Queries] pointer, and provides a [WithTx]
// helper for transactional sequences.
//
// A zero Store is unusable; construct one with [NewStore].
type Store struct {
	*Queries
	pool *pgxpool.Pool
}

// NewStore constructs a Store from a pgx connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{
		Queries: New(pool),
		pool:    pool,
	}
}

// Pool returns the underlying connection pool. Use sparingly — prefer the
// query methods or [WithTx]. Provided for tests and import tooling's
// pgx.CopyFrom batch path.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// WithTx runs fn inside a database transaction. On any error returned by fn
// (or panic), the transaction is rolled back. On nil return, it is committed.
// The Queries instance passed to fn is bound to the transaction; queries
// issued through it participate in the same tx.
func (s *Store) WithTx(ctx context.Context, fn func(q *Queries) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	if err := fn(s.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
