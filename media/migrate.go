package media

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationsTableName is media's dedicated goose version table, distinct from
// the host application's default goose_db_version so the two migration
// histories never collide when they share a database.
const migrationsTableName = "media_goose_db_version"

// Migrate runs the embedded schema migrations against pool. It is safe to call
// repeatedly; goose tracks applied versions in media_goose_db_version.
//
// It uses goose.Provider rather than the package-level globals so it does not
// mutate process-wide goose state the host may also rely on.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("media: migrate: %w", err)
	}
	sqldb := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqldb.Close() }()

	prov, err := goose.NewProvider(goose.DialectPostgres, sqldb, sub,
		goose.WithTableName(migrationsTableName),
		goose.WithLogger(goose.NopLogger()),
	)
	if err != nil {
		return fmt.Errorf("media: migrate: %w", err)
	}
	if _, err := prov.Up(ctx); err != nil {
		return fmt.Errorf("media: migrate: %w", err)
	}
	return nil
}
