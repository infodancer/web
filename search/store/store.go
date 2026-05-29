// Package store is the data-access layer: the sqlc-generated queries plus a
// thin [Store] wrapper over a pgx connection pool with a transaction helper.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the high-level data-access entry point for the search module. It
// wraps a pgx connection pool, exposes every sqlc-generated query method
// directly via the embedded [*Queries] pointer, and provides a [WithTx]
// helper for transactional sequences.
//
// A zero Store is unusable; construct one with [NewStore].
type Store struct {
	*Queries
	pool *pgxpool.Pool
}

// NewStore constructs a Store from a pgx connection pool. When standalone,
// `serve` passes the read-only query role's pool here; the indexer passes the
// read-write role's pool (DESIGN §10).
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{
		Queries: New(pool),
		pool:    pool,
	}
}

// Pool returns the underlying connection pool. Use sparingly — prefer the
// query methods or [WithTx]. Provided for tests and the seed/import paths.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// WithTx runs fn inside a database transaction. On any error returned by fn
// the transaction is rolled back; on nil return it is committed. The Queries
// instance passed to fn is bound to the transaction.
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
