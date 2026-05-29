package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/search"
)

// runSeed ensures the schema exists (runs migrations) and loads a SQL fixture
// of sample documents so the UI has something to return while the indexer is
// still unbuilt. Uses the read-write (indexer) role.
func runSeed(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	var (
		configPath = fs.String("config", "", "path to TOML config")
		dsn        = fs.String("db", "", "Postgres DSN (overrides config/env)")
		file       = fs.String("file", "testdata/seed.sql", "SQL fixture to load")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	fc, err := loadFileConfig(*configPath)
	if err != nil {
		return err
	}
	db := firstNonEmpty(*dsn, fc.DB.IndexerDSN, os.Getenv("DATABASE_URL"))
	if db == "" {
		return fmt.Errorf("no database DSN (set --db, db.indexer_dsn, or DATABASE_URL)")
	}

	sqlBytes, err := os.ReadFile(*file) //nolint:gosec // operator-supplied fixture path
	if err != nil {
		return fmt.Errorf("read fixture: %w", err)
	}

	pool, err := pgxpool.New(ctx, db)
	if err != nil {
		return fmt.Errorf("postgres connect: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Ensure the schema exists before loading the fixture.
	if _, err := search.New(search.Config{DB: pool, Logger: logger, AutoMigrate: true}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("load fixture: %w", err)
	}

	var n int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM search_documents WHERE deleted_at IS NULL").Scan(&n); err != nil {
		return fmt.Errorf("count: %w", err)
	}
	logger.Info("seeded", "fixture", *file, "live_documents", n)
	return nil
}
