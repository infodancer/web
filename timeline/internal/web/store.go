package web

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/timeline/db"
)

// PgStore wraps a pgxpool and sqlc Queries for database access.
type PgStore struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewPgStore runs pending migrations, then connects to the database and returns a PgStore.
func NewPgStore(ctx context.Context, databaseURL string) (*PgStore, error) {
	if err := db.Migrate(databaseURL); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &PgStore{
		pool:    pool,
		queries: db.New(pool),
	}, nil
}

// Close shuts down the database connection pool.
func (s *PgStore) Close() {
	s.pool.Close()
}

// Queries returns the sqlc queries for external use (e.g. sync).
func (s *PgStore) Queries() *db.Queries {
	return s.queries
}
